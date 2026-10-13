# -*- coding: utf-8 -*-
"""relocate — 阶段2：归位（修正落盘分类）。

抓取时可能把静态资源（URL 含 /assets/）误存进 _api/；本模块按 reqlog 把它们
按原 URL 归位到 _assets/ 的镜像路径下。
"""

from __future__ import annotations

import os

from .store import read_reqlog, url_to_relpath


def relocate(store_dir: str) -> int:
    """把误存进 _api/ 的静态资源按原路径归位到 _assets/。返回归位数。"""
    reqlog = os.path.join(store_dir, "_reqlog.jsonl")
    assets_dir = os.path.join(store_dir, "_assets")

    print("\n" + "=" * 64)
    print("阶段2/4 归位：修正资源落盘分类")
    print("=" * 64)

    records = read_reqlog(reqlog)
    if not records:
        print("  无 reqlog，跳过")
        return 0

    moved = 0
    for r in records:
        url = r.get("url") or ""
        saved = r.get("saved") or ""
        if not saved or "/assets/" not in url or "_api" not in saved:
            continue
        src = os.path.join(store_dir, saved)
        if not os.path.isfile(src):
            continue
        dst = os.path.join(assets_dir, url_to_relpath(url))
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        with open(src, "rb") as fi, open(dst, "wb") as fo:
            fo.write(fi.read())
        moved += 1

    print(f"  归位 {moved} 个静态资源 -> _assets/")
    return moved
