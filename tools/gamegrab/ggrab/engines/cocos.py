# -*- coding: utf-8 -*-
"""engines.cocos — Cocos Creator 适配器。

Cocos 产物落盘规律（实测 cm.darknight.games/scriptMaster 全量抓取验证）：
    assets/<pack>/config.json                        —— 该 pack 的清单
    assets/<pack>/import/<uuid 前2位>/<uuid>.json    —— 每个「独立」资源的导入描述
    assets/<pack>/import/<聚合名前2位>/<聚合名>.json —— 打包装的聚合文件（含多个资源）
    assets/<pack>/native/<uuid 前2位>/<uuid>.<ext>   —— 真实二进制

config.json 三个关键字段（实测）：
    types : ["cc.Prefab","cc.JsonAsset","cc.SpriteFrame","cc.Texture2D",
             "sp.SkeletonData","cc.Asset","cc.AudioClip","cc.SpriteAtlas"]
    uuids : 资源 uuid 列表（release 模式为 22 位压缩 uuid，需 decode_uuid 还原）
    packs : {"聚合名": [uuids 下标数组]}，被包含的下标不再有独立 import json
    paths : {"下标": [逻辑路径, types 下标, ...]} —— 每个资源的确切类型！

类型 → native 扩展名映射（实测证据见 E_execution.md）：
    cc.Texture2D     -> .png / .jpg （实测 7252/bg 为 jpg）
    cc.AudioClip     -> .mp3
    cc.Asset         -> .atlas   （Spine 的 atlas 描述）
    cc.ParticleAsset -> .plist
    sp.SkeletonData  -> 无独立 native（骨骼数据即 import json 本身，实测全空）
    cc.SpriteFrame   -> 无独立 native（图集内帧，靠所在图集承载）
    cc.SpriteAtlas   -> 无独立 native（靠其 Texture2D 承载）
    cc.Prefab        -> 无 native（纯 json）
    cc.JsonAsset     -> 无 native（纯 json）

特殊：config.json 的 uuids 里存在「无 paths 登记」的下标（实测）：
    - 若在 redirect 里（形如 [26,0]）→ 重定向别名，无任何独立文件，跳过；
    - 否则若被打进 packs → 内容在聚合 json 内，无独立文件；
    - 否则（loose，未被聚合）→ **独立资源**，必须有 import/<uuid>.json
      （运行时实测直接请求该路径，源站存在；旧实现误当裸贴图找 native 而漏抓）。
"""

from __future__ import annotations

import os
import re

from .base import EngineAdapter, Manifest, MissingItem

# 类型 → native 候选扩展名（按优先级；无 native 的类型不出现在表中）
# 实测：cc.Texture2D 可能是 png 也可能是 jpg（如 7252/bg 为 .jpg），故给候选列表
TYPE_EXTS = {
    "cc.Texture2D": ["png", "jpg"],
    "cc.AudioClip": ["mp3"],
    "cc.Asset": ["atlas"],
    "cc.ParticleAsset": ["plist"],
}

# 「无 paths 登记」的下标不再有统一定位规则（见 _parse_cfg 的 loose 判定）：
#   - packed 者 → 内容在聚合 json 内；
#   - loose 者 → 独立资源，走 import/<uuid>.json（实测运行时直接请求）。

# Cocos 压缩 uuid（base64）解码表，标准表定义于引擎 core/utils/misc
_B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
_BASE64_VALUES = [0] * 256
for _i, _ch in enumerate(_B64):
    _BASE64_VALUES[ord(_ch)] = _i
_HEX = list("0123456789abcdef")
_R4 = ["", "", "", ""]
# 36 位标准 uuid 模板：8-4-4-4-12（'-' 为分隔位）
_TMPL = _R4 + _R4 + ["-"] + _R4 + ["-"] + _R4 + ["-"] + _R4 + ["-"] + _R4 + _R4 + _R4
# 模板中非 '-' 的位置（hex 位）
_TMPL_IDX = [i for i, t in enumerate(_TMPL) if t != "-"]


def decode_uuid(u: str) -> str:
    """把 Cocos release 模式的压缩 uuid（22 位 base64）还原为 36 位标准 uuid。

    移植自引擎 decodeUuid：非 22 位（即已是标准 uuid）原样返回。
    """
    if len(u) != 22:
        return u
    s = list(_TMPL)
    s[0] = u[0]
    s[1] = u[1]
    ri = 2
    for e in range(2, 22, 2):
        a = _BASE64_VALUES[ord(u[e])]
        c = _BASE64_VALUES[ord(u[e + 1])]
        s[_TMPL_IDX[ri]] = _HEX[a >> 2]
        ri += 1
        s[_TMPL_IDX[ri]] = _HEX[((3 & a) << 2) | (c >> 4)]
        ri += 1
        s[_TMPL_IDX[ri]] = _HEX[15 & c]
        ri += 1
    return "".join(s)


class CocosAdapter(EngineAdapter):
    name = "cocos"
    priority = 50

    def __init__(self):
        #: 已解析的 config.json 缓存：pack 名 -> 归一化后的清单视图
        self._cfgs = {}

    # ---------- 识别 ----------
    @classmethod
    def detect(cls, web_root: str, reqlog: list, site_base: str = "") -> int:
        """Cocos 特征：存在 assets/<pack>/config.json 且含 uuids/packs 字段；
        或存在 cocos2d-js-min.js / cc.* 引擎文件；或 reqlog 里出现 assets/*/import/ 路径。
        """
        score = 0
        # 1) 本地产物特征：先定位 web 根（含 assets/ 的目录），再看 assets/<pack>/config.json
        roots = []
        if web_root and os.path.isdir(web_root):
            roots.append(web_root)
            sub = cls().find_web_root(web_root)
            if sub and sub != web_root:
                roots.append(sub)
        for root in roots:
            ax = os.path.join(root, "assets")
            if not os.path.isdir(ax):
                continue
            for n in os.listdir(ax):
                cfg = os.path.join(ax, n, "config.json")
                if os.path.isfile(cfg):
                    data = cls._read_json(cfg) or {}
                    if isinstance(data, dict) and ("uuids" in data or "packs" in data):
                        score = max(score, 90)
                    else:
                        score = max(score, 60)
        if web_root:
            for root in roots:
                for fn in ("cocos2d-js-min.js", "cocos-js", "cc.js"):
                    if os.path.isfile(os.path.join(root, fn)):
                        score = max(score, 70)
        # 2) reqlog 特征
        for r in reqlog or []:
            u = (r.get("url") or "")
            if re.search(r"/assets/[^/]+/(import|native)/", u):
                score = max(score, 80)
                break
        return score

    # ---------- 清单 ----------
    def collect_manifests(self, web_root: str) -> list:
        """扫描 assets/<pack>/config.json，收集所有 pack 清单。"""
        manifests = []
        if not web_root:
            return manifests
        ax = os.path.join(web_root, "assets")
        if not os.path.isdir(ax):
            return manifests
        for n in sorted(os.listdir(ax)):
            cfg = os.path.join(ax, n, "config.json")
            if os.path.isfile(cfg):
                manifests.append(Manifest(
                    pack=n,
                    rel_path=f"assets/{n}/config.json",
                    data=self._read_json(cfg) or {},
                ))
        return manifests

    def discover_bundles(self, web_root: str, api_bodies: list) -> list:
        """从接口响应里挖掘「动态 bundle」候选（通用启发式，不写死字段名）。

        原理：Cocos 的运行时动态资源包，其目录名就是 bundle 名，且该名字
        通常**直接来自服务端接口返回值**（如关卡包名 = 关卡 id）。因此只要把
        接口响应里出现的「像包名的标量」全量收集，逐一探测 assets/<名>/config.json
        是否存在，即可把没被抓到的动态包补齐——无需人工逐一点击。

        候选判定（保守，宁多勿漏，探测阶段会过滤）：
          - 纯数字 token：长度 >= 3（如 8080 / 8153），排除 0/1 等常见开关值；
          - 形如包名的短标识：字母数字下划线组合、长度 >= 2 且含至少一位数字
            （如 "lv8153" / "pack01"），避免把纯中文/纯字母误当包名。
        递归遍历 JSON 的所有标量与数组元素；同时用正则兜底扫描形如
        "assets/<名>/" 的字符串，覆盖接口里直接给出路径的场景。
        """
        import json
        import re
        cands = set()

        def walk(node):
            if isinstance(node, dict):
                for v in node.values():
                    walk(v)
            elif isinstance(node, list):
                for v in node:
                    walk(v)
            elif isinstance(node, bool):
                return
            elif isinstance(node, int):
                s = str(node)
                if len(s) >= 3:
                    cands.add(s)
            elif isinstance(node, float):
                if node.is_integer():
                    s = str(int(node))
                    if len(s) >= 3:
                        cands.add(s)
            elif isinstance(node, str):
                s = node.strip()
                if re.fullmatch(r"\d{3,}", s):
                    cands.add(s)
                elif re.fullmatch(r"[A-Za-z][A-Za-z0-9_]{1,}", s) and re.search(r"\d", s):
                    cands.add(s)

        for body in api_bodies or []:
            if not body:
                continue
            text = body.decode("utf-8", errors="replace") if isinstance(body, (bytes, bytearray)) else str(body)
            try:
                walk(json.loads(text))
            except Exception:
                pass
            # 兜底：接口里直接出现 assets/<名>/ 路径
            for m in re.finditer(r"assets/([A-Za-z0-9_]+)/", text):
                cands.add(m.group(1))

        return [f"assets/{c}" for c in sorted(cands)]

    def find_web_root(self, assets_root: str) -> str | None:
        """Cocos 的 web 根 = 含 assets/ 子目录的目录，优先带 index.html 者。"""
        if not os.path.isdir(assets_root):
            return None
        best = None
        for root, _dirs, files in os.walk(assets_root):
            if os.path.isdir(os.path.join(root, "assets")):
                has_index = "index.html" in files
                if best is None or (has_index and not best[0]):
                    best = (has_index, root)
        return best[1] if best else None

    # ---------- 清单解析（核心） ----------
    @staticmethod
    def _parse_cfg(manifest: Manifest) -> dict:
        """把一份 config.json 解析成「下标 -> 资源描述」的归一化视图。

        返回 {idx: {"uuid": 36位uuid, "type": 类型名, "packed": 聚合名|None,
                    "redirect": bool, "bare": bool, "loose": bool}}
        依据：
          - uuids[i] 给出该下标的压缩 uuid（decode_uuid 还原）
          - paths[i] = [逻辑路径, types 下标, ...] 给出该下标的确切类型
          - packs 的 {聚合名: [下标...]} 标记哪些下标被打进聚合文件
          - redirect 形如 [26,0, 30,1]：偶数位是「重定向下标」，其资源实体在别处，
            该下标无任何独立文件（实测 import/native 全 404），须跳过
          - 无 paths 且非 redirect 的下标分两类（实测）：
              * packed（被打进聚合文件）→ 内容在聚合 json 内，无独立文件；
              * loose（未被打包！）→ **独立资源**，其 import json 位于
                import/<uuid>.json（运行时实测会直接请求该路径，源站存在）。
        历史缺陷：旧实现把所有「无 paths」下标一律当裸贴图去找 native，
        导致 loose 型独立资源（如 Pool idx43/45）被彻底漏抓——正是「卡加载页」
        的根因。bare 字段保留为「无 paths」的统称，loose 才是新增的关键判定。
        """
        data = manifest.data or {}
        types = data.get("types") or []
        uuids = data.get("uuids") or []
        paths = data.get("paths") or {}
        packs = data.get("packs") or {}
        # 重定向下标集合（redirect 数组偶数位）
        redirect_idx = set()
        red = data.get("redirect") or []
        if isinstance(red, list):
            for k in range(0, len(red), 2):
                if isinstance(red[k], int):
                    redirect_idx.add(red[k])
        # 被聚合覆盖的下标 -> 聚合名
        packed_of = {}
        for pk, idxs in packs.items():
            if isinstance(idxs, list):
                for i in idxs:
                    if isinstance(i, int):
                        packed_of[i] = pk
        out = {}
        for i, u in enumerate(uuids):
            tn = ""
            has_path = False
            p = paths.get(str(i))
            if isinstance(p, list) and len(p) >= 2 and isinstance(p[1], int):
                has_path = True
                if 0 <= p[1] < len(types):
                    tn = types[p[1]]
            packed = packed_of.get(i)
            out[i] = {
                "uuid": decode_uuid(u) if isinstance(u, str) else "",
                "type": tn,
                "packed": packed,
                "redirect": i in redirect_idx,
                "bare": (not has_path) and (i not in redirect_idx),
                # loose：无 paths、非 redirect、且未被打包 —— 独立资源，
                # 必须有 import/<uuid>.json（实测运行时直接请求该路径）
                "loose": (not has_path) and (i not in redirect_idx)
                         and packed is None,
            }
        return out

    def resolve_missing(self, manifest: Manifest, web_root: str) -> list:
        """由 config.json 推导该 pack 下所有「应存在但本地缺失」的资源。

        规则（全部实测验证）：
          1) 每个 pack 的聚合文件 import/<前2>/<聚合名>.json 必须存在；
          2) 未被任何 pack 覆盖的下标 —— 其独立 import json 必须存在；
          3) 下标若属于「有 native 的类型」（见 TYPE_EXT）—— 其 native 必须存在。
        被聚合覆盖的下标没有独立 import json，且若为 cc.SpriteFrame
        （图集内帧）也没有独立 native，天然被跳过，不会产生假 404。
        """
        out = []
        data = manifest.data or {}
        packs = data.get("packs") or {}
        info = self._parse_cfg(manifest)
        p2 = manifest.pack

        # 0) bundle 脚本入口 index.js：每个 bundle 的必备文件。
        #    运行时 Cocos 会直接请求 assets/<pack>/index.js 加载该 bundle 的逻辑脚本；
        #    缺失会抛 ERROR 4928 并导致关卡/资源包整体加载失败。
        #    旧实现只推导 import/native，漏了这个入口——正是「动态发现的 bundle
        #    全部卡在 index.js 404」的根因。
        irel_js = f"assets/{p2}/index.js"
        if not self._exists(web_root, irel_js):
            out.append(MissingItem(irel_js, kind="script"))

        # 1) 聚合文件本身
        for pk in packs:
            rel = f"assets/{p2}/import/{pk[:2]}/{pk}.json"
            if not self._exists(web_root, rel):
                out.append(MissingItem(rel, kind="import"))

        # 2) 独立 import json + native
        for i in sorted(info):
            ent = info[i]
            uid = ent["uuid"]
            if not uid or ent["redirect"]:
                continue
            # 2.1 独立 import json：
            #     - packed 的跳过（内容在聚合 json 内）
            #     - loose（无 paths 未打包的独立资源）必须抓——实测运行时直请求
            #     - 有 paths 的非 packed 资源同样必须抓
            if not ent["packed"]:
                irel = f"assets/{p2}/import/{uid[:2]}/{uid}.json"
                if not self._exists(web_root, irel):
                    out.append(MissingItem(irel, kind="import"))
            # 2.2 native：仅「有 native 的类型」；候选扩展名按优先级试，
            #     命中任一即视为已存在，全未命中才判缺失——主路径取第一个候选，
            #     其余候选填入 fallbacks 供补全器回退抓取
            #     注意：loose 型（无类型登记）不再盲目假定为贴图，避免假 404
            exts = TYPE_EXTS.get(ent["type"])
            if exts and not ent["bare"]:
                if not any(self._exists(
                        web_root, f"assets/{p2}/native/{uid[:2]}/{uid}.{e}")
                        for e in exts):
                    base = f"assets/{p2}/native/{uid[:2]}/{uid}"
                    out.append(MissingItem(
                        f"{base}.{exts[0]}", kind="native",
                        fallbacks=[f"{base}.{e}" for e in exts[1:]]))
        return out

    # ---------- 二次推导：聚合/独立 import json 内部的资源 ----------
    def resolve_natives(self, manifest: Manifest, rel_path: str, raw: bytes,
                        web_root: str) -> list:
        """由已下载的 import json 内容，推导其中被引用资源的 import/native。

        import json 结构（实测 2033f808...json / 010f46986.json）是一个数组：
            [ ver, [uuid1, uuid2, ...], [字段名...], [类型定义...], ... ]
        index 1 是「本文件引用的所有资源 uuid」。

        为何必须展开：一个 import json 会把其依赖（图集纹理、Spine 的
        atlas/skeleton、音效等）描述揉进来，但那些依赖的 **独立 import json
        与 native 二进制并不在聚合内**，必须按引用逐个补齐
        —— 这正是「游戏卡加载页」的根因。

        关键改进（历次实测修正）：
          - 不再盲试 20 种后缀（旧实现制造海量假 404）；
          - 用本 pack 的 config.json 查出每个 uuid 的确切类型 -> 候选扩展名；
          - sp.SkeletonData 无独立 native（骨骼数据即 import json 本身）；
          - 处理 redirect（无独立文件）与 bare（无 paths 登记的裸贴图）；
          - 仅处理「本 pack 已知」的 uuid，跨包引用不臆造（避免假缺失）。
        """
        arr = self._loads(raw)
        out = []
        if arr is None:
            return out

        # 本 pack 的 下标/类型表：uuid -> 资源描述
        info = self._parse_cfg(manifest)
        ent_of = {e["uuid"]: e for e in info.values() if e["uuid"]}

        uids = set()
        if isinstance(arr, list) and len(arr) > 1 and isinstance(arr[1], list):
            for it in arr[1]:
                if isinstance(it, str):
                    u = self._norm_uuid(it)
                    if u:
                        uids.add(u)

        p2 = manifest.pack
        for uid in sorted(uids):
            ent = ent_of.get(uid)
            if ent is None or ent["redirect"]:
                # 跨包引用 / 重定向别名：本 pack 无独立文件，不臆造
                continue
            # 1) 独立 import json：非 packed 者（含 loose 独立资源）必须有
            if not ent["packed"]:
                irel = f"assets/{p2}/import/{uid[:2]}/{uid}.json"
                if not self._exists(web_root, irel):
                    out.append(MissingItem(irel, kind="import"))
            # 2) native：仅在该 uuid 的类型有 native 且非 bare 时才推导；
            #    候选扩展名全未命中才判缺失，主路径取第一个候选，
            #    其余候选填入 fallbacks 供补全器回退抓取
            exts = TYPE_EXTS.get(ent["type"])
            if exts and not ent["bare"]:
                if not any(self._exists(
                        web_root, f"assets/{p2}/native/{uid[:2]}/{uid}.{e}")
                        for e in exts):
                    base = f"assets/{p2}/native/{uid[:2]}/{uid}"
                    out.append(MissingItem(
                        f"{base}.{exts[0]}", kind="native",
                        fallbacks=[f"{base}.{e}" for e in exts[1:]]))
        return out

    # ---------- 工具 ----------
    @staticmethod
    def _loads(raw: bytes):
        import json
        try:
            return json.loads(raw.decode("utf-8", errors="replace"))
        except Exception:
            return None

    @staticmethod
    def _norm_uuid(s: str) -> str:
        """把紧凑/标准 uuid（可带 @sub）规范为 36 位标准 uuid。"""
        if not isinstance(s, str) or not s:
            return ""
        s = s.split("@", 1)[0]
        if len(s) == 36 and s[8] == "-":
            return s
        if len(s) in (22, 23):
            return decode_uuid(s[:22])
        return ""
