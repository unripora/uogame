# -*- coding: utf-8 -*-
"""complete — 阶段3：补全（引擎识别 + 适配器调度 + 通用回退）。

流程：
    1. 定位 web_root（优先引擎适配器，失败则通用直觉）
    2. 自动识别引擎（engines.detect_engine）
    3. 引擎清单补全（adapter.collect_manifests + resolve_missing，两趟抓取）
    4. 通用回退补全（generic：reqlog 重放 + JS/JSON 扫描）

设计上本模块「只面向适配器接口编程」，不认识任何具体引擎。
"""

from __future__ import annotations

import concurrent.futures as cf
import json
import os
import re
from urllib.parse import urlparse

from .engines import detect_engine, get_adapter
from .engines.base import Manifest, MissingItem
from .http import http_get
from .store import read_reqlog, read_api_bodies

# 补全并发线程数
FETCH_WORKERS = 12
# 清单补全最大轮次（多趟至收敛：上一趟下载的 import json 会二次推导出新的 native）
MAX_ROUNDS = 5


class Completer:
    def __init__(self, store_dir: str, cookies: str = "", workers: int = FETCH_WORKERS):
        self.store_dir = os.path.abspath(store_dir)
        self.assets_dir = os.path.join(self.store_dir, "_assets")
        self.reqlog_path = os.path.join(self.store_dir, "_reqlog.jsonl")
        self.cookies = cookies
        self.workers = workers
        self.reqlog = read_reqlog(self.reqlog_path)
        self.site_base = ""
        self.web_root = ""
        self.adapter = None
        self.engine = "generic"
        self.scores = {}
        self.ok = 0
        #: 已探测但源站不存在（404/失败）的相对路径，落盘供诊断扣除假缺失
        self.probed_404 = set()

    # ---------- 定位 ----------
    def detect_site_base(self) -> str:
        """推断游戏资源站 base（形如 http://host/path/）。

        优先级：
          1) summary.json 的 url（抓取入口，最权威）
          2) reqlog 里的 web-mobile 路径
          3) reqlog 里首个 URL 的 host
        """
        # 1) summary.json
        sp = os.path.join(self.store_dir, "summary.json")
        if os.path.isfile(sp):
            try:
                import json
                with open(sp, encoding="utf-8") as f:
                    url = (json.load(f) or {}).get("url") or ""
                m = re.match(r"^(https?://[^/]+/.*?/web-mobile/)[^/]*$", url)
                if m:
                    return m.group(1)
                m2 = re.match(r"^(https?://[^/]+)/(.*/)?[^/]*$", url)
                if m2:
                    # 回退：保留到入口文件所在目录
                    base = url[: url.rfind("/") + 1]
                    if "/web-mobile/" in base:
                        return base
            except Exception:
                pass
        # 2) reqlog
        best = ""
        for r in self.reqlog:
            url = r.get("url") or ""
            m = re.search(r"^(https?://[^/]+/.*?/web-mobile/[^/]*$)", url)
            if m:
                return m.group(1)[: m.group(1).rfind("/") + 1]
            m2 = re.match(r"^(https?://[^/]+)/", url)
            if m2 and not best:
                best = m2.group(1) + "/"
        return best

    def find_web_root(self) -> str:
        """先试引擎适配器定位，再退回「含 index.html 的目录」。"""
        best = None
        for root, _dirs, files in os.walk(self.assets_dir):
            if "index.html" in files and not best:
                best = root
        return best or ""

    # ---------- 抓取 ----------
    def _get_save(self, rel: str, kind: str = "asset") -> bool:
        """按 site_base + rel 抓取并落盘到 web_root/rel。"""
        data, code = http_get(self.site_base + rel, cookies=self.cookies)
        if data is None:
            return False
        fp = os.path.join(self.web_root, rel.replace("/", os.sep))
        os.makedirs(os.path.dirname(fp), exist_ok=True)
        with open(fp, "wb") as f:
            f.write(data)
        self.ok += 1
        return data is not None

    def _fetch_batch(self, items: list, on_done=None) -> dict:
        """并发抓取一批 MissingItem；on_done(rel, data) 可在下载后二次解析。

        抓取失败的项（源站 404 / 请求错误）记入 self.probed_404，
        供诊断阶段区分「本地下不到」与「源站本就没有」。

        扩展名回退：若 item.rel_path 抓取失败，且 item.fallbacks 非空，
        则按序改试备用路径（同名前缀不同扩展名）。用于「同一资源的扩展名
        不唯一」场景——如 Cocos 贴图候选 png/jpg，源站实际只存其一。
        命中某一候选即落盘该路径，避免因只试第一个候选而漏抓。
        """
        got = {}
        lock = __import__("threading").Lock()

        def work(item):
            # 候选路径 = 主路径 + 备用路径；按序尝试，首个成功即止
            candidates = [item.rel_path] + list(getattr(item, "fallbacks", []) or [])
            for rel in candidates:
                data, _ = http_get(self.site_base + rel, cookies=self.cookies)
                if data is None:
                    continue
                fp = os.path.join(self.web_root, rel.replace("/", os.sep))
                os.makedirs(os.path.dirname(fp), exist_ok=True)
                with open(fp, "wb") as f:
                    f.write(data)
                with lock:
                    self.ok += 1
                    got[rel] = data
                if on_done:
                    on_done(rel, data)
                return
            # 全部候选均失败：记录首个路径为「源站不存在」
            with lock:
                self.probed_404.add(item.rel_path)

        with cf.ThreadPoolExecutor(max_workers=self.workers) as ex:
            list(ex.map(work, items))
        return got

    # ---------- 引擎清单补全 ----------
    def complete_by_manifests(self, extra_manifests: list = None) -> int:
        """清单补全：多趟循环至收敛。

        为何要多趟：Cocos 等引擎的 native 资源依赖 import json 内容二次推导，
        第一趟下载 import json 后才能推导出对应 native；下载后再触发下一趟。
        单趟会漏掉这部分，故循环直到某趟无新增（或达上限）。

        关键：每趟都必须「重扫本地已存在的聚合 import json」并展开。
        旧实现只在 on_done（本趟新下载项）里触发 resolve_natives，
        导致上一趟已落盘、但同批其它文件尚未就绪的聚合文件永远不会被再次展开
        —— 这正是「资源没抓全」的收敛性缺陷。

        extra_manifests：由动态包发现（discover_dynamic_bundles）新增的清单，
        与本地扫描出的清单合并，一并纳入补全。
        """
        manifests = self.adapter.collect_manifests(self.web_root)
        if extra_manifests:
            have = {m.pack for m in manifests}
            for m in extra_manifests:
                if m.pack not in have:
                    manifests.append(m)
        if not manifests:
            return 0
        before = self.ok
        for _rnd in range(1, MAX_ROUNDS + 1):
            got_this = 0
            for mf in manifests:
                missing = self.adapter.resolve_missing(mf, self.web_root)
                if missing:
                    got_this += len(self._fetch_batch(missing))
                # 主动展开「本地已存在」的聚合 import json（补下载其引用资源）
                got_this += self._expand_local_aggs(mf)
            if got_this == 0:
                break
        return self.ok - before

    def _expand_local_aggs(self, mf: Manifest) -> int:
        """扫描本 pack 本地已存在的 import json，展开其中引用，补齐 native。

        Cocos 等引擎会把多个资源聚合进一个 import json；其引用的 native
        必须逐个补齐。此步与「是否本趟新下载」无关，故每趟都要执行。
        """
        if not hasattr(self.adapter, "resolve_natives"):
            return 0
        root = os.path.join(self.web_root, "assets", mf.pack, "import")
        if not os.path.isdir(root):
            return 0
        got = 0
        for dirpath, _dirs, files in os.walk(root):
            for fn in files:
                if not fn.endswith(".json"):
                    continue
                fp = os.path.join(dirpath, fn)
                rel = os.path.relpath(fp, self.web_root).replace(os.sep, "/")
                try:
                    with open(fp, "rb") as f:
                        raw = f.read()
                except Exception:
                    continue
                second = self.adapter.resolve_natives(mf, rel, raw, self.web_root)
                if second:
                    got += len(self._fetch_batch(second))
        return got

    # ---------- 通用回退补全 ----------
    def complete_by_fallback(self) -> int:
        generic = get_adapter("generic")
        before = self.ok
        # 路径1：reqlog 重放
        replay = generic.replay_targets(self.web_root, self.reqlog, self.site_base)
        if replay:
            self._fetch_batch(replay)
        # 路径2：JS/JSON 扫描
        scan = generic.scan_targets(self.web_root)
        if scan:
            self._fetch_batch(scan)
        return self.ok - before

    def _save_probed_404(self):
        """把「已探测但源站不存在」的相对路径落盘，供诊断扣除假缺失。"""
        import json
        fp = os.path.join(self.store_dir, "_probed_404.json")
        try:
            with open(fp, "w", encoding="utf-8") as f:
                json.dump(sorted(self.probed_404), f, ensure_ascii=False)
        except Exception:
            pass

    # ---------- 动态资源包发现 ----------
    def discover_dynamic_bundles(self) -> list:
        """从接口数据反推动态资源包，探测并下载其清单，返回新增的 Manifest 列表。

        流程：
          1) 收集本地接口响应原文；
          2) 交适配器 discover_bundles() 得到候选包目录（引擎无关）；
          3) 逐个探测 {候选}/config.json 是否存在（HTTP 200 视为命中）；
          4) 命中则落盘 config.json 并构造 Manifest，交由既有清单补全流程递归补齐。
        """
        if not hasattr(self.adapter, "discover_bundles") or not self.site_base:
            return []
        try:
            bodies = read_api_bodies(self.store_dir)
        except Exception:
            bodies = []
        if not bodies:
            return []
        try:
            cands = self.adapter.discover_bundles(self.web_root, bodies) or []
        except Exception:
            cands = []
        if not cands:
            return []

        new_manifests = []
        # 已有 pack 跳过（本地已存在 config.json 的交给 collect_manifests 处理）
        existing = set()
        ax = os.path.join(self.web_root, "assets")
        if os.path.isdir(ax):
            existing = set(os.listdir(ax))

        for cdir in sorted(set(cands)):
            pack = cdir.split("/")[-1]
            if not pack or pack in existing:
                continue
            rel_cfg = f"{cdir}/config.json"
            if self.adapter._exists(self.web_root, rel_cfg):
                existing.add(pack)
                continue
            data, code = http_get(self.site_base + rel_cfg, cookies=self.cookies)
            if data is None:
                self.probed_404.add(rel_cfg)
                continue
            fp = os.path.join(self.web_root, rel_cfg.replace("/", os.sep))
            os.makedirs(os.path.dirname(fp), exist_ok=True)
            with open(fp, "wb") as f:
                f.write(data)
            self.ok += 1
            existing.add(pack)
            try:
                parsed = json.loads(data.decode("utf-8", errors="replace"))
            except Exception:
                parsed = {}
            new_manifests.append(Manifest(pack=pack, rel_path=rel_cfg, data=parsed or {}))
        if new_manifests:
            print(f"  动态包发现: 命中 {len(new_manifests)} 个（来自接口数据反推）")
        return new_manifests

    # ---------- 主流程 ----------
    def run(self, force_engine: str = "", use_fallback: bool = True) -> dict:
        print("\n" + "=" * 64)
        print("阶段3/4 补全：引擎识别 + 清单补全（+ 通用回退）")
        print("=" * 64)

        self.site_base = self.detect_site_base().rstrip("/") + "/"
        self.web_root = self.find_web_root()
        if not self.web_root:
            print("  未找到游戏 web 根目录，跳过补全")
            return {"engine": "none", "ok": 0}

        # 引擎识别
        if force_engine:
            self.adapter = get_adapter(force_engine)
            self.scores = {force_engine: -1}
        if self.adapter is None:
            self.adapter, self.scores = detect_engine(
                self.web_root, self.reqlog, self.site_base)
        self.engine = self.adapter.name

        print(f"  原站 base : {self.site_base}")
        print(f"  本地 web  : {self.web_root}")
        print(f"  引擎识别  : {self.engine}  (scores={self.scores})")

        # 动态资源包发现：从接口数据反推「首屏未加载、运行时才加载」的资源包
        extra = self.discover_dynamic_bundles()
        n_manifest = self.complete_by_manifests(extra)
        print(f"  清单补全  : {n_manifest} 个")
        n_fb = 0
        if use_fallback:
            n_fb = self.complete_by_fallback()
            print(f"  通用回退  : {n_fb} 个")
        print(f"  合计补全  : {self.ok} 个")
        # 记录「源站也不存在」的探测结果，供诊断扣除假缺失
        self._save_probed_404()
        return {
            "engine": self.engine,
            "engine_scores": self.scores,
            "site_base": self.site_base,
            "web_root": os.path.relpath(self.web_root, self.store_dir).replace("\\", "/"),
            "ok": self.ok,
            "by_manifest": n_manifest,
            "by_fallback": n_fb,
        }
