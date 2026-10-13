# -*- coding: utf-8 -*-
"""pack — 阶段4：打包（生成离线补丁 + config.json + zip）。

行为对齐重构前 grab.py 的 pack_game，并接入 shim.py 的三合一补丁：
    - 从 _reqlog.jsonl 收集非 /assets/ 的接口响应 -> _apidata/ + _apimap.json
    - 从 _wslog.json 收集 WebSocket 会话 -> _wsmap.json
    - 注入离线补丁到 index.html
    - 写 config.json（轻游戏导入配置）
    - 压成 zip
"""

from __future__ import annotations

import json
import os
import shutil
import zipfile

from .engines import detect_engine
from .shim import build_shim
from .store import read_reqlog, url_key


def build_offline_shim(api_host: str, api_map: dict, ws_map: dict) -> str:
    return build_shim(api_host, api_map, ws_map)


def _load_ws_map(store_dir: str) -> dict:
    """读取 _wslog.json，转成 wsmap：绝对 URL -> {sent, recv}。"""
    fp = os.path.join(store_dir, "_wslog.json")
    if not os.path.isfile(fp):
        return {}
    try:
        with open(fp, encoding="utf-8") as f:
            sessions = json.load(f)
    except Exception:
        return {}
    out = {}
    for s in sessions or []:
        url = s.get("url")
        if url and url not in out:
            out[url] = {"sent": s.get("sent", []), "recv": s.get("recv", [])}
    return out


def pack_game(store_dir: str, name: str = "", game_id: str = "") -> str:
    """生成离线补丁 + config.json，压成可导入 zip。返回 zip 路径（失败返回 ""）。"""
    store_dir = os.path.abspath(store_dir)
    print("\n" + "=" * 64)
    print("阶段4/4 打包：生成离线补丁并压缩为可导入 zip")
    print("=" * 64)

    web_root = _find_web_root(store_dir)
    if not web_root:
        print("  未找到游戏 web 根目录，无法打包")
        return ""

    name = name or (f"game{game_id}" if game_id else "gamegrab")
    build = os.path.join(store_dir, f"_build_{name}")
    if os.path.isdir(build):
        shutil.rmtree(build)
    shutil.copytree(web_root, build)

    # 1) 收集接口响应 -> _apidata + _apimap
    apidata = os.path.join(build, "_apidata")
    os.makedirs(apidata, exist_ok=True)
    api_map = {}
    api_host = ""
    idx = 0
    reqlog_path = os.path.join(store_dir, "_reqlog.jsonl")
    api_records = _collect_api_records(store_dir, reqlog_path)
    missing = 0
    for url, saved in api_records:
        if not saved or "/assets/" in url:
            continue
        src = os.path.join(store_dir, saved)
        if not os.path.isfile(src):
            missing += 1
            print(f"  [警告] 接口快照缺失，已跳过：{url}  ({saved})")
            continue
        if not api_host:
            from urllib.parse import urlparse
            api_host = urlparse(url).netloc
        idx += 1
        fn = f"api_{idx:04d}.json"
        shutil.copyfile(src, os.path.join(apidata, fn))
        key = url_key(url)
        if key not in api_map:
            api_map[key] = f"_apidata/{fn}"

    ws_map = _load_ws_map(store_dir)

    with open(os.path.join(build, "_apimap.json"), "w", encoding="utf-8") as f:
        json.dump(api_map, f, ensure_ascii=False, indent=1)
    if ws_map:
        with open(os.path.join(build, "_wsmap.json"), "w", encoding="utf-8") as f:
            json.dump(ws_map, f, ensure_ascii=False, indent=1)

    # 2) 注入离线补丁
    idx_html = os.path.join(build, "index.html")
    if os.path.isfile(idx_html):
        with open(idx_html, encoding="utf-8") as f:
            html = f.read()
        shim = build_shim(api_host, api_map, ws_map)
        if "<head>" in html:
            html = html.replace("<head>", "<head>\n" + shim, 1)
        else:
            html = shim + html
        with open(idx_html, "w", encoding="utf-8") as f:
            f.write(html)

    # 3) config.json（轻游戏导入配置）
    config = {
        "name": name,
        "cover_path": "cover.png",
        "category_names": ["益智"],
        "tags": ["H5"],
        "sort_order": 0,
        "is_hidden": 0,
        "is_landscape": 0,
    }
    with open(os.path.join(build, "config.json"), "w", encoding="utf-8") as f:
        json.dump(config, f, ensure_ascii=False, indent=1)

    # 4) 压 zip
    out_zip = os.path.join(store_dir, f"{name}.zip")
    if os.path.exists(out_zip):
        os.remove(out_zip)
    n = 0
    with zipfile.ZipFile(out_zip, "w", zipfile.ZIP_DEFLATED) as z:
        for root, _dirs, files in os.walk(build):
            for fn in files:
                fp = os.path.join(root, fn)
                rel = os.path.relpath(fp, build).replace("\\", "/")
                z.write(fp, rel)
                n += 1

    print(f"  游戏名   : {name}")
    print(f"  接口映射 : {len(api_map)} 条")
    if missing:
        print(f"  [注意]   : {missing} 条接口快照缺失，未纳入映射（将走本地空成功兜底）")
    print(f"  WS 映射  : {len(ws_map)} 条")
    print(f"  文件数   : {n}")
    print(f"  输出 zip : {out_zip}")
    return out_zip


def _collect_api_records(store_dir: str, reqlog_path: str) -> list:
    """收集 (url, saved_relpath) 接口记录。

    优先用 _reqlog.jsonl；若为空（如重跑补全后 reqlog 不可用），
    退回 summary.json 的 suspects（已含 url -> _api/ 落盘路径）。
    """
    records = []
    for r in read_reqlog(reqlog_path):
        url = r.get("url") or ""
        saved = r.get("saved") or ""
        if not saved:
            continue
        if not (r.get("ctype", "").startswith("application/json") or "_api" in saved):
            continue
        records.append((url, saved))
    if records:
        return records
    # 兜底：summary.json 的 suspects
    sp = os.path.join(store_dir, "summary.json")
    if os.path.isfile(sp):
        try:
            with open(sp, encoding="utf-8") as f:
                for s in (json.load(f) or {}).get("suspects") or []:
                    url = s.get("url") or ""
                    saved = s.get("path") or ""
                    if url and saved:
                        records.append((url, saved))
        except Exception:
            pass
    return records


def _find_web_root(store_dir: str) -> str:
    """优先用引擎适配器定位 web 根；失败退回含 index.html 的目录。"""
    assets_dir = os.path.join(store_dir, "_assets")
    reqlog = read_reqlog(os.path.join(store_dir, "_reqlog.jsonl"))
    adapter, _ = detect_engine(assets_dir, reqlog)
    root = adapter.find_web_root(assets_dir)
    if root:
        return root
    for r, _d, files in os.walk(assets_dir):
        if "index.html" in files:
            return r
    return ""
