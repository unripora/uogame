# -*- coding: utf-8 -*-
"""query — CLI 查询子命令（供人/AI 调用，输出可解析 JSON）。

支持：
    --list-packs   列出识别的引擎与 pack 清单
    --list-apis    列出所有接口端点 + 是否已映射
    --grep <kw>    在 reqlog / 落盘 JS-JSON 中检索关键字
    --missing      列出缺失资源
    --show <rel>   查看落盘文件内容摘要
"""

from __future__ import annotations

import json
import os
from urllib.parse import urlparse

from .engines import detect_engine
from .store import read_reqlog, url_key


def _ctx(store_dir: str):
    store_dir = os.path.abspath(store_dir)
    assets_dir = os.path.join(store_dir, "_assets")
    reqlog = read_reqlog(os.path.join(store_dir, "_reqlog.jsonl"))
    adapter, scores = detect_engine(assets_dir, reqlog)
    web_root = adapter.find_web_root(assets_dir) or ""
    return store_dir, assets_dir, reqlog, adapter, scores, web_root


def list_packs(store_dir: str) -> dict:
    _sd, _ad, _rl, adapter, scores, web_root = _ctx(store_dir)
    manifests = adapter.collect_manifests(web_root) if web_root else []
    return {
        "engine": adapter.name,
        "engine_scores": scores,
        "web_root": os.path.relpath(web_root, _sd).replace("\\", "/") if web_root else "",
        "packs": [{"pack": m.pack, "config": m.rel_path} for m in manifests],
    }


def list_apis(store_dir: str) -> dict:
    sd, _ad, reqlog, _ap, _sc, _wr = _ctx(store_dir)
    apis = []
    for r in reqlog:
        url = r.get("url") or ""
        if "/assets/" in url:
            continue
        if not (r.get("ctype", "").startswith("application/json") or "_api" in (r.get("saved") or "")):
            continue
        apis.append({
            "method": r.get("method"),
            "url": url,
            "status": r.get("status"),
            "key": url_key(url),
            "saved": r.get("saved"),
        })
    return {"count": len(apis), "apis": apis}


def grep(store_dir: str, keyword: str, limit: int = 50) -> dict:
    sd = os.path.abspath(store_dir)
    kw = keyword.lower()
    hits = []
    # 1) 在 reqlog 的 url / post_data 里找
    for r in read_reqlog(os.path.join(sd, "_reqlog.jsonl")):
        blob = f"{r.get('url','')} {r.get('post_data') or ''}".lower()
        if kw in blob:
            hits.append({"where": "reqlog", "url": r.get("url"), "saved": r.get("saved")})
            if len(hits) >= limit:
                return {"keyword": keyword, "count": len(hits), "hits": hits}
    # 2) 在 _api/ 与 _assets/ 的文本文件里找
    for base in ("_api", "_assets"):
        root_dir = os.path.join(sd, base)
        for root, _d, files in os.walk(root_dir):
            for fn in files:
                if not fn.lower().endswith((".json", ".js", ".txt", ".xml", ".html")):
                    continue
                fp = os.path.join(root, fn)
                try:
                    with open(fp, encoding="utf-8", errors="replace") as f:
                        text = f.read()
                except Exception:
                    continue
                if kw in text.lower():
                    i = text.lower().find(kw)
                    hits.append({
                        "where": base,
                        "file": os.path.relpath(fp, sd).replace("\\", "/"),
                        "snippet": text[max(0, i - 60): i + 120].replace("\n", " "),
                    })
                    if len(hits) >= limit:
                        return {"keyword": keyword, "count": len(hits), "hits": hits}
    return {"keyword": keyword, "count": len(hits), "hits": hits}


def missing(store_dir: str) -> dict:
    _sd, _ad, _rl, adapter, scores, web_root = _ctx(store_dir)
    out = []
    if web_root:
        for m in adapter.collect_manifests(web_root):
            for item in adapter.resolve_missing(m, web_root):
                out.append({"rel": item.rel_path, "kind": item.kind, "pack": m.pack})
    return {"engine": adapter.name, "missing_count": len(out), "missing": out[:500]}


def show(store_dir: str, rel_path: str, maxlen: int = 2000) -> dict:
    sd = os.path.abspath(store_dir)
    fp = os.path.join(sd, rel_path.replace("/", os.sep))
    if not os.path.isfile(fp):
        return {"error": "not found", "path": rel_path}
    size = os.path.getsize(fp)
    try:
        with open(fp, encoding="utf-8", errors="replace") as f:
            text = f.read(maxlen)
    except Exception as e:
        return {"path": rel_path, "size": size, "error": str(e)}
    return {"path": rel_path, "size": size, "preview": text}


def dispatch(store_dir: str, args) -> dict:
    """根据 CLI 参数分派到对应查询，返回结构化结果。"""
    if getattr(args, "list_packs", False):
        return list_packs(store_dir)
    if getattr(args, "list_apis", False):
        return list_apis(store_dir)
    if getattr(args, "grep", None):
        return grep(store_dir, args.grep)
    if getattr(args, "missing", False):
        return missing(store_dir)
    if getattr(args, "show", None):
        return show(store_dir, args.show)
    return {"error": "no query specified",
            "usage": "query <dir> [--list-packs|--list-apis|--grep kw|--missing|--show rel]"}


def print_result(result: dict, pretty: bool = True):
    print(json.dumps(result, ensure_ascii=False, indent=2 if pretty else None))
