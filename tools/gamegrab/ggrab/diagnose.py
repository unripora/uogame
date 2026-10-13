# -*- coding: utf-8 -*-
"""diagnose — 可玩性诊断：给出结构化报告 + 0-100 可玩性评分。

面向「人/AI 查询这个包能不能玩、卡在哪」。
"""

from __future__ import annotations

import os
from urllib.parse import urlparse

from .engines import detect_engine
from .store import read_reqlog, read_api_bodies


def _dynamic_bundle_status(adapter, web_root: str, packages_present: list, store_dir: str) -> dict:
    """从接口数据反推动态资源包，与本地已抓包对比，得出「疑似缺失的动态包」。

    通用能力：不写死任何游戏字段，候选由适配器 discover_bundles 从接口响应挖掘，
    本地已有的包目录视为「已抓」；其余候选即「可能漏抓的动态包」，供补齐参考。
    """
    result = {"detected": 0, "present": 0, "missing": [], "examples": []}
    if not hasattr(adapter, "discover_bundles") or not web_root:
        return result
    try:
        bodies = read_api_bodies(store_dir)
    except Exception:
        bodies = []
    if not bodies:
        return result
    try:
        cands = adapter.discover_bundles(web_root, bodies) or []
    except Exception:
        cands = []
    if not cands:
        return result

    present_names = set(packages_present)
    missing = []
    for cdir in cands:
        name = cdir.split("/")[-1]
        if name in present_names:
            continue
        missing.append(name)
    result["detected"] = len(cands)
    result["present"] = len(present_names & {c.split("/")[-1] for c in cands})
    result["missing"] = missing[:200]
    result["examples"] = missing[:10]
    return result


def diagnose(store_dir: str) -> dict:
    store_dir = os.path.abspath(store_dir)
    assets_dir = os.path.join(store_dir, "_assets")
    reqlog = read_reqlog(os.path.join(store_dir, "_reqlog.jsonl"))

    adapter, scores = detect_engine(assets_dir, reqlog)
    web_root = adapter.find_web_root(assets_dir) or ""

    # 清单
    manifests = adapter.collect_manifests(web_root) if web_root else []
    packs = [m.pack for m in manifests]

    # 缺失资源（按各适配器推导）
    missing = []
    for m in manifests:
        for item in adapter.resolve_missing(m, web_root):
            missing.append({"rel": item.rel_path, "kind": item.kind})

    # 扣除「补全阶段已探测但源站也不存在」的假缺失
    probed_404 = set()
    p404 = os.path.join(store_dir, "_probed_404.json")
    if os.path.isfile(p404):
        try:
            import json
            with open(p404, encoding="utf-8") as f:
                probed_404 = set(json.load(f))
        except Exception:
            probed_404 = set()
    missing = [m for m in missing if m["rel"] not in probed_404]

    # 接口端点 + 是否会被打包映射
    apimap_keys = set()
    mp = os.path.join(store_dir, "_build_%s" % _guess_build_name(store_dir), "_apimap.json")
    if os.path.isfile(mp):
        try:
            import json
            with open(mp, encoding="utf-8") as f:
                apimap_keys = set(json.load(f).keys())
        except Exception:
            pass

    apis = []
    for r in reqlog:
        url = r.get("url") or ""
        if "/assets/" in url:
            continue
        if not (r.get("ctype", "").startswith("application/json") or "_api" in (r.get("saved") or "")):
            continue
        from .store import url_key
        k = url_key(url)
        apis.append({"url": url, "mapped": k in apimap_keys or not apimap_keys})

    unmapped = [a for a in apis if not a["mapped"]]

    # 错误：404 / 请求失败
    errors = []
    for r in reqlog:
        st = r.get("status")
        if st == -1:
            errors.append({"url": r.get("url"), "reason": r.get("failure", "requestfailed")})
        elif isinstance(st, int) and st >= 400:
            errors.append({"url": r.get("url"), "reason": f"HTTP {st}"})

    # 关键 JS 是否 404（index.html 引用的脚本）
    critical_404 = [e for e in errors
                    if (e.get("url") or "").lower().endswith(".js")]

    # 评分
    score = 100
    if not web_root:
        score -= 50
    if adapter.name == "generic":
        score -= 15
    score -= min(30, len(missing))
    score -= min(20, len(unmapped) * 2)
    score -= min(20, len(critical_404) * 5)

    # 动态资源包状态（从接口数据反推，通用能力）
    dyn = _dynamic_bundle_status(adapter, web_root, packs, store_dir)
    # 动态包缺失也计入扣分（每缺 10 个包 -2 分，最多 -20）
    score -= min(20, (len(dyn["missing"]) // 10) * 2)
    score = max(0, min(100, score))

    report = {
        "store_dir": store_dir,
        "engine": adapter.name,
        "engine_scores": scores,
        "web_root": os.path.relpath(web_root, store_dir).replace("\\", "/") if web_root else "",
        "packs": packs,
        "api_endpoints": len(apis),
        "unmapped_apis": [a["url"] for a in unmapped],
        "missing_assets": missing[:200],
        "missing_count": len(missing),
        "dynamic_bundles": dyn,
        "errors": errors[:100],
        "error_count": len(errors),
        "critical_js_404": [e["url"] for e in critical_404],
        "playable_score": score,
    }
    return report


def _guess_build_name(store_dir: str) -> str:
    for n in os.listdir(store_dir):
        if n.startswith("_build_"):
            return n[len("_build_"):]
    return ""


def print_report(rep: dict):
    print("=" * 64)
    print("可玩性诊断")
    print("=" * 64)
    print(f"  抓取目录   : {rep['store_dir']}")
    print(f"  引擎识别   : {rep['engine']}  {rep['engine_scores']}")
    print(f"  游戏 web 根: {rep['web_root'] or '(未定位)'}")
    print(f"  pack 清单  : {len(rep['packs'])} -> {rep['packs']}")
    print(f"  接口端点   : {rep['api_endpoints']}（未映射 {len(rep['unmapped_apis'])}）")
    print(f"  缺失资源   : {rep['missing_count']}")
    print(f"  请求错误   : {rep['error_count']}（关键 JS 404: {len(rep['critical_js_404'])}）")
    dyn = rep.get("dynamic_bundles") or {}
    if dyn.get("detected"):
        print(f"  动态资源包 : 接口反推候选 {dyn['detected']} 个，已抓 {dyn['present']} 个，"
              f"疑似缺失 {len(dyn.get('missing') or [])} 个")
    print(f"  >>> 可玩性评分: {rep['playable_score']} / 100")
    if dyn.get("missing"):
        print()
        print("  ⚠️  提醒：检测到「运行时按接口数据逐个下发的动态资源包」，以下疑似漏抓——")
        print("     这些包不会在首屏加载，人工点击也易漏；请执行二次补抓（complete 阶段已自动处理）：")
        for n in dyn.get("examples") or []:
            print(f"       - {n}")
        more = len(dyn["missing"]) - len(dyn.get("examples") or [])
        if more > 0:
            print(f"       ... 另有 {more} 个")
    if rep["critical_js_404"]:
        print("  关键 JS 404：")
        for u in rep["critical_js_404"][:5]:
            print(f"    - {u}")
    if rep["unmapped_apis"]:
        print("  未映射接口（打包后将走网络，需关注）：")
        for u in rep["unmapped_apis"][:8]:
            print(f"    - {u}")
