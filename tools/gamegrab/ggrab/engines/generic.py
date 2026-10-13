# -*- coding: utf-8 -*-
"""engines.generic — 通用回退适配器（不依赖任何引擎特征）。

三路补全：
    1. reqlog 重放：把 reqlog 中出现过、且本地缺失的 URL 逐个重抓
       —— 覆盖「人工没点到」的懒加载资源。
    2. JS/JSON 扫描：从已落盘的 JS/JSON 文本里正则提取资源路径，尝试补抓。
    3. 目录探测：对已知资源目录做有限探测（可选，带超时保护）。

reqlog 重放与扫描是「相对 web_root 的路径列表」；complete.py 负责实际抓取。
"""

from __future__ import annotations

import os
import re
from urllib.parse import urlparse

from .base import EngineAdapter, Manifest, MissingItem

# 从 JS/JSon 文本里提取疑似资源路径（相对路径 + 常见资源后缀）
_PATH_RE = re.compile(
    r"""["'`(]((?:/?[\w.\-@]+/)*[\w.\-@]+\.(?:png|jpg|jpeg|gif|webp|mp3|ogg
        |wav|m4a|mp4|webm|json|plist|atlas|skel|fnt|bin|ttf|woff2?|wasm
        |xml|txt|tmx|tsx|astc|pkm))["'`)]""",
    re.IGNORECASE | re.VERBOSE,
)


class GenericAdapter(EngineAdapter):
    name = "generic"
    priority = 0

    @classmethod
    def detect(cls, web_root: str, reqlog: list, site_base: str = "") -> int:
        """回退适配器：始终以 1 分的兜底置信度参与（永不因它「胜出」而误判引擎）。"""
        return 1

    # ---------- 路径1：reqlog 重放 ----------
    def replay_targets(self, web_root: str, reqlog: list, site_base: str) -> list:
        """收集 reqlog 中出现过、但本地缺失、且属于同站资源站的 URL 相对路径。"""
        out = []
        seen = set()
        host = urlparse(site_base).netloc if site_base else ""
        for r in reqlog or []:
            url = r.get("url") or ""
            if not url.startswith("http"):
                continue
            u = urlparse(url)
            if host and u.netloc != host:
                continue
            if not _looks_static(u.path):
                continue
            rel = u.path.lstrip("/")
            if not rel or rel in seen:
                continue
            seen.add(rel)
            if not self._exists(web_root, rel):
                out.append(MissingItem(rel, kind="replay"))
        return out

    # ---------- 路径2：JS/JSON 扫描 ----------
    #: 扫描时跳过的「引擎清单/诊断产物」目录与文件名：
    #:   - assets/<pack>/import、assets/<pack>/native 是 Cocos 等引擎的资源
    #:     描述区，其 json 内的 atlas 文本 / uuid 引用会被误当资源路径
    #:     （实测产生 assets/8154/import/20/qiang.png 等假路径）；
    #:   - config.json / _probed_404.json / _apimap.json 同理，非资源路径容器。
    _SKIP_DIRS = ("import", "native")
    _SKIP_FILES = ("config.json", "_probed_404.json", "_apimap.json")

    def scan_targets(self, web_root: str) -> list:
        """扫描已落盘的 JS/JSON，提取其中的资源相对路径，补抓缺失者。"""
        out = []
        seen = set()
        for root, dirs, files in os.walk(web_root):
            # 剪掉引擎资源描述区，避免把 uuid/atlas 文本误当路径
            dirs[:] = [d for d in dirs if d not in self._SKIP_DIRS]
            for fn in files:
                if not fn.lower().endswith((".js", ".json", ".html", ".htm")):
                    continue
                if fn in self._SKIP_FILES:
                    continue
                fp = os.path.join(root, fn)
                base_rel = os.path.relpath(root, web_root).replace("\\", "/")
                if base_rel == ".":
                    base_rel = ""
                try:
                    with open(fp, encoding="utf-8", errors="replace") as f:
                        text = f.read()
                except Exception:
                    continue
                for m in _PATH_RE.finditer(text):
                    p = m.group(1)
                    if p.startswith("http") or p.startswith("data:"):
                        continue
                    rel = p.lstrip("./").lstrip("/") if p.startswith(("/", "./")) else p
                    # 相对当前文件目录解析
                    if not p.startswith("/") and base_rel:
                        rel = f"{base_rel}/{p}"
                    rel = rel.replace("//", "/")
                    if rel in seen:
                        continue
                    seen.add(rel)
                    if _looks_static(rel) and not self._exists(web_root, rel):
                        out.append(MissingItem(rel, kind="scan"))
        return out


def _looks_static(path: str) -> bool:
    """判断路径是否像静态资源（按扩展名）。"""
    ext = os.path.splitext(path)[1].lower()
    return ext in {
        ".js", ".mjs", ".css", ".png", ".jpg", ".jpeg", ".gif", ".webp",
        ".bmp", ".svg", ".ico", ".mp3", ".ogg", ".wav", ".m4a", ".mp4",
        ".webm", ".ttf", ".woff", ".woff2", ".otf", ".eot", ".wasm", ".json",
        ".xml", ".txt", ".atlas", ".skel", ".plist", ".fnt", ".bin", ".data",
        ".tmx", ".tsx", ".astc", ".pkm",
    }
