# -*- coding: utf-8 -*-
"""store — 落盘布局、reqlog 读写、URL↔路径映射。

对外提供：
    Store 类       : 管理单个抓取目录的落盘（_assets/_api/_reqlog.jsonl/summary）
    工具函数       : sanitize / url_to_relpath / read_reqlog / iter_reqlog
"""

import json
import os
import re
import threading
from datetime import datetime
from urllib.parse import urlparse, unquote

# ============================================================
# 常量（全局共享）
# ============================================================

# 静态资源扩展名（判断是否按资源镜像落盘）
STATIC_EXT = {
    ".js", ".mjs", ".css", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp",
    ".svg", ".ico", ".mp3", ".ogg", ".wav", ".m4a", ".mp4", ".webm", ".ttf",
    ".woff", ".woff2", ".otf", ".eot", ".wasm", ".json", ".xml", ".txt",
    ".atlas", ".skel", ".plist", ".fnt", ".bin", ".data", ".tmx", ".tsx",
    ".html", ".htm",
}

# 接口响应类型（单独存 _api）
API_MIME = ("application/json", "text/json", "application/x-javascript")

# 关卡清单关键字（用于结束分析）
LEVEL_KEYS = ["level", "levels", "kaiguan", "guanka", "关卡", "stage", "map", "chapter"]


# ============================================================
# 路径工具
# ============================================================

def sanitize(name: str, maxlen: int = 120) -> str:
    """把 URL 路径片段转为安全的本地文件名。"""
    name = unquote(name or "")
    name = re.sub(r'[<>:"|?*\x00-\x1f]', "_", name)
    name = name.strip().strip(".")
    if not name:
        name = "_"
    if len(name) > maxlen:
        # 保留尾部原始名，避免超长
        name = name[-maxlen:]
    return name


def url_to_relpath(url: str) -> str:
    """把 URL 转成镜像用的相对路径：<host>/<path>?<query 摘要>"""
    u = urlparse(url)
    host = u.netloc or "unknown"
    path = u.path or "/"
    if path.endswith("/"):
        path += "index.html"
    parts = [sanitize(p) for p in path.split("/") if p != ""]
    rel = os.path.join(sanitize(host), *parts)
    if u.query:
        q = sanitize(u.query, 60)
        root, ext = os.path.splitext(rel)
        rel = f"{root}__{q}{ext}"
    return rel


def url_key(url: str) -> str:
    """去掉协议与主机，仅保留 path?query，作为接口映射键。"""
    return re.sub(r"^https?://[^/]+", "", url or "")


def read_reqlog(path: str) -> list:
    """读取 _reqlog.jsonl，返回记录列表（坏行跳过）。"""
    records = []
    if not os.path.isfile(path):
        return records
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                records.append(json.loads(line))
            except Exception:
                continue
    return records


def read_api_bodies(store_dir: str) -> list:
    """收集本地已落盘的接口响应原文（非 /assets/），供引擎挖掘动态资源包。

    来源优先级：
      1) _reqlog.jsonl 中非 /assets/ 的响应快照
      2) summary.json 的 suspects（reqlog 缺失时兜底）
      3) _api / _apidata 目录下所有 *.json 快照（最终兜底）
    返回 bytes 列表。
    """
    store_dir = os.path.abspath(store_dir)
    bodies = []
    seen = set()

    def _add(fp: str):
        rp = os.path.abspath(fp)
        if rp in seen or not os.path.isfile(rp):
            return
        seen.add(rp)
        try:
            with open(rp, "rb") as f:
                bodies.append(f.read())
        except Exception:
            pass

    for r in read_reqlog(os.path.join(store_dir, "_reqlog.jsonl")):
        url = r.get("url") or ""
        saved = r.get("saved") or ""
        if not saved or "/assets/" in url:
            continue
        _add(os.path.join(store_dir, saved))

    sp = os.path.join(store_dir, "summary.json")
    if os.path.isfile(sp):
        try:
            with open(sp, encoding="utf-8") as f:
                for s in (json.load(f) or {}).get("suspects") or []:
                    saved = s.get("path") or ""
                    if saved:
                        _add(os.path.join(store_dir, saved))
        except Exception:
            pass

    for sub in ("_api", "_apidata"):
        d = os.path.join(store_dir, sub)
        if not os.path.isdir(d):
            continue
        for fn in sorted(os.listdir(d)):
            if fn.endswith(".json"):
                _add(os.path.join(d, fn))
    return bodies


# ============================================================
# Store：单个抓取目录的落盘管理
# ============================================================

class Store:
    def __init__(self, out_dir: str):
        self.out_dir = os.path.abspath(out_dir)
        self.assets_dir = os.path.join(self.out_dir, "_assets")
        self.api_dir = os.path.join(self.out_dir, "_api")
        self.reqlog_path = os.path.join(self.out_dir, "_reqlog.jsonl")
        self._lock = threading.Lock()
        self._reqlog_f = None
        self._seq = 0
        # 统计
        self.count = {"assets": 0, "api": 0, "failed": 0, "other": 0}

    # ---------- 生命周期 ----------
    def prepare(self):
        for d in (self.out_dir, self.assets_dir, self.api_dir):
            os.makedirs(d, exist_ok=True)
        self._reqlog_f = open(self.reqlog_path, "w", encoding="utf-8")

    def close(self):
        if self._reqlog_f:
            try:
                self._reqlog_f.close()
            finally:
                self._reqlog_f = None

    # ---------- 落盘 ----------
    def write_asset(self, rel_path: str, data: bytes) -> str:
        """按镜像相对路径写入 _assets/，返回相对 out_dir 的路径。"""
        full = os.path.join(self.assets_dir, rel_path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "wb") as f:
            f.write(data)
        return os.path.relpath(full, self.out_dir)

    def write_api(self, url: str, body: bytes) -> str:
        """把接口响应单独存到 _api/，返回相对 out_dir 的路径。"""
        seq = self.next_seq()
        host = urlparse(url).netloc
        base = sanitize(os.path.basename(urlparse(url).path) or "api", 60)
        name = f"{seq:04d}_{sanitize(host)}_{base}"
        if not name.lower().endswith(".json"):
            name += ".json"
        full = os.path.join(self.api_dir, name)
        with open(full, "wb") as f:
            f.write(body)
        return os.path.relpath(full, self.out_dir)

    def log_req(self, record: dict):
        if not self._reqlog_f:
            return
        with self._lock:
            self._reqlog_f.write(json.dumps(record, ensure_ascii=False) + "\n")
            self._reqlog_f.flush()

    def next_seq(self) -> int:
        with self._lock:
            self._seq += 1
            return self._seq

    # ---------- 汇总 ----------
    def write_summary(self, extra: dict) -> str:
        summary = {
            "out_dir": self.out_dir,
            "count": self.count,
            "finished_at": datetime.now().isoformat(timespec="seconds"),
        }
        summary.update(extra or {})
        sp = os.path.join(self.out_dir, "summary.json")
        with open(sp, "w", encoding="utf-8") as f:
            json.dump(summary, f, ensure_ascii=False, indent=2)
        return sp
