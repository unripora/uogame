# -*- coding: utf-8 -*-
"""engines.base — 引擎适配器统一接口与数据结构。

设计目标：把「补全」从「写死 Cocos」升级为「可插拔引擎适配器」。
每个适配器只需回答四个问题：
    1. detect()          这段抓取产物是不是我（置信度 0-100）
    2. find_web_root()   游戏根目录（index.html 所在目录）在哪
    3. collect_manifests() 我认得哪些资源清单（用于精确推导缺失资源）
    4. resolve_missing() 由清单推导「应该存在但本地缺失」的资源相对路径

补全器（complete.py）只面向本接口编程，不认识任何具体引擎。
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass
class Manifest:
    """一份「资源清单」的抽象。

    不同引擎的清单形态不同，适配器负责把自家清单解析成统一的
    「相对路径 -> 是否本地已存在」视图，补全器照单抓取即可。

    pack     : 逻辑分组名（Cocos 下即 assets/<pack>/ 的 pack 名）
     rel_path : 清单文件相对 web_root 的路径（如 assets/main/config.json）
     data     : 解析后的清单内容（JSON 对象，可为空）
    """

    pack: str
    rel_path: str
    data: dict = field(default_factory=dict)


@dataclass
class MissingItem:
    """一条「应存在但本地缺失」资源。

    rel_path   : 相对 web_root 的路径（补全器据此拼 base URL 抓取）
    kind       : import / native / manifest / probe，仅用于诊断归类
    fallbacks  : 备用相对路径（同名前缀、不同扩展名）。当 rel_path 抓取失败
                 （源站 404）时，补全器按序改试这些路径——用于「同一资源的
                 扩展名不唯一」场景（如 Cocos 贴图可能是 png 也可能是 jpg）。
    """

    rel_path: str
    kind: str = "asset"
    fallbacks: list = field(default_factory=list)


class EngineAdapter:
    """引擎适配器抽象基类。

    子类须设置 ``name``，并至少实现 ``detect`` 与 ``resolve_missing``；
    其余方法有默认实现（返回空），便于只做「识别」的轻量适配器。
    """

    name: str = "generic"
    #: 优先级的次要因子：分数相同时，priority 大者胜出（generic 恒为最低）
    priority: int = 0

    # ---------- 识别 ----------
    @classmethod
    def detect(cls, web_root: str, reqlog: list, site_base: str = "") -> int:
        """返回置信度 0-100；<=0 表示「不是我」。"""
        return 0

    # ---------- 定位 ----------
    def find_web_root(self, assets_root: str) -> str | None:
        """在 _assets 镜像目录中定位游戏根目录（含 index.html 的目录）。

        默认实现：优先「同时含 index.html 与 assets/ 子目录」的目录，
        否则退回含 index.html 的目录；均无则返回 None。
        """
        candidates = []
        if not os.path.isdir(assets_root):
            return None
        for root, _dirs, files in os.walk(assets_root):
            if "index.html" in files:
                has_assets = os.path.isdir(os.path.join(root, "assets"))
                candidates.append((has_assets, root))
        if not candidates:
            return None
        candidates.sort(key=lambda x: (x[0], len(x[1])), reverse=True)
        return candidates[0][1]

    # ---------- 清单 ----------
    def collect_manifests(self, web_root: str) -> list:
        """收集该引擎的资源清单。默认无清单。"""
        return []

    # ---------- 动态资源包发现 ----------
    def discover_bundles(self, web_root: str, api_bodies: list) -> list:
        """从「已抓到的接口响应原文」中挖掘运行时才会加载的动态资源包候选。

        为何需要：某些引擎（如 Cocos）在初始化后，会依据服务端返回的数据
        动态加载额外的资源包（关卡包 / 资源包 / DLC 等）。这些包在抓取时
        不会出现在首屏清单里，也无法靠人工逐个点全，只能由「接口数据」反推。

        契约：
          - 入参 api_bodies：所有非 /assets/ 的接口响应原文（bytes 列表）
          - 返回：候选**包相对目录**列表（相对 web_root，如 ["assets/8080"]），
            由调用方逐个探测其清单文件是否存在，命中才纳入补全。
          - 默认实现返回空；是否可信由适配器自行把握（宁可多候选，探测会过滤）。

        注意：实现必须是**通用启发式**，不得写死某个游戏的字段名或取值。
        """
        return []

    # ---------- 推导缺失 ----------
    def resolve_missing(self, manifest: Manifest, web_root: str) -> list:
        """由清单推导「应存在但本地缺失」的资源。默认无。"""
        return []

    # ---------- 辅助 ----------
    @staticmethod
    def _exists(web_root: str, rel_path: str) -> bool:
        return os.path.isfile(os.path.join(web_root, rel_path.replace("/", os.sep)))

    @staticmethod
    def _read_json(path: str):
        import json
        try:
            with open(path, encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            return None
