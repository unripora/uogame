# -*- coding: utf-8 -*-
"""capture — 阶段1：浏览器驱动 + 全量 hook + 落盘。

要点（踩坑固化）：
  * 不使用 page.route 逐请求拦截（会卡网络，表现为"抓包时网络中断"）；
    改用启动参数禁缓存。
  * sync API 禁止在事件回调里读 response.body()，回调只入队，主线程 drain 读取。
  * 用 page.wait_for_timeout() 驱动事件循环（time.sleep 会漏投递回调）。
  * context.on("page", ...) 给新标签页/弹窗也挂 hook。
"""

import json
import threading
import time
from collections import deque
from datetime import datetime
from urllib.parse import urlparse

from playwright.sync_api import sync_playwright

from .store import Store, API_MIME, LEVEL_KEYS, url_to_relpath, read_reqlog


class Capture:
    def __init__(self, url: str, store: Store, game_id: str = ""):
        self.url = url
        self.store = store
        self.game_id = game_id

        self.count = store.count
        self.api_records = []          # 结束分析用
        self.stop_flag = threading.Event()
        self.pending_responses = deque()
        self.pending_posts = {}
        # 离线补丁 / 补全 所需
        self.api_host_hint = ""
        self.cookies_jar = ""
        self.ws_log = []               # 记录 WebSocket 会话（打包补全用）
        # 交互模式（先浏览、后抓取）
        self.record_flag = threading.Event()   # 置位后才开始记录（挂 hook 生效）
        self.recording = False

    # ---------- 事件 hook ----------
    def on_request(self, request):
        try:
            if request.method in ("POST", "PUT", "PATCH"):
                self.pending_posts[id(request)] = request.post_data
        except Exception:
            pass

    def on_response(self, response):
        """仅入队；body 由主线程 drain() 读取。未开始记录时不入队。"""
        if not self.recording:
            return
        try:
            url = response.url
            if url.startswith("data:") or url.startswith("blob:"):
                return
            self.pending_responses.append(response)
        except Exception:
            pass

    def drain(self):
        while self.pending_responses:
            response = self.pending_responses.popleft()
            try:
                self.process_response(response)
            except Exception as e:
                print(f"  [ERR] process_response 异常: {e}")

    def process_response(self, response):
        request = response.request
        url = response.url
        status = response.status
        headers = {}
        try:
            headers = response.headers
        except Exception:
            pass
        ctype = (headers.get("content-type", "") or "").lower()
        post_data = self.pending_posts.pop(id(request), None)

        try:
            body = response.body()
        except Exception:
            body = None

        rel_path = url_to_relpath(url)
        is_api = any(m in ctype for m in API_MIME)
        # URL 含 /assets/ 的一律按镜像落盘（Cocos 的 config.json 也是静态资源）
        in_assets = "/assets/" in urlparse(url).path

        saved_path = None
        if body is not None:
            if in_assets:
                saved_path = self.store.write_asset(rel_path, body)
                self.count["assets"] += 1
            elif is_api or "json" in rel_path.lower():
                host = urlparse(url).netloc
                if host and not self.api_host_hint:
                    self.api_host_hint = host
                saved_path = self.store.write_api(url, body)
                self.count["api"] += 1
                self.api_records.append({
                    "url": url, "status": status, "ctype": ctype,
                    "path": saved_path, "text": self._try_text(body),
                })
            else:
                saved_path = self.store.write_asset(rel_path, body)
                self.count["assets"] += 1

        self.store.log_req({
            "ts": datetime.now().isoformat(timespec="milliseconds"),
            "method": request.method,
            "url": url,
            "status": status,
            "ctype": ctype,
            "resource_type": request.resource_type,
            "post_data": post_data,
            "saved": saved_path,
        })

        marker = "API" if is_api else "   "
        print(f"  [{marker}] {status} {request.method} {self._short(url)} -> {saved_path or '(no body)'}")

    def on_request_failed(self, request):
        if not self.recording:
            return
        try:
            self.count["failed"] += 1
            self.store.log_req({
                "ts": datetime.now().isoformat(timespec="milliseconds"),
                "method": request.method,
                "url": request.url,
                "status": -1,
                "failure": str(request.failure),
                "resource_type": request.resource_type,
            })
            print(f"  [FAIL] {request.method} {self._short(request.url)} :: {request.failure}")
        except Exception:
            pass

    def on_console(self, msg):
        if msg.type in ("error", "warning"):
            print(f"  [console.{msg.type}] {msg.text}")

    def on_pageerror(self, err):
        print(f"  [pageerror] {err}")

    def on_websocket(self, ws):
        """记录 WebSocket 全部收发帧（打包时生成 WS 离线回放）。未记录时忽略。"""
        if not self.recording:
            return
        print(f"  [ws.open] {ws.url}")
        sess = {"url": ws.url, "sent": [], "recv": [], "ts": datetime.now().isoformat(timespec="milliseconds")}
        self.ws_log.append(sess)

        def _rec(payload, kind):
            data = payload if isinstance(payload, str) else str(payload)
            sess[kind].append(data[:100000])

        try:
            ws.on("framereceived", lambda p: _rec(p, "recv"))
            ws.on("framesent", lambda p: _rec(p, "sent"))
            ws.on("close", lambda *_: sess.setdefault("closed", True))
        except Exception:
            pass

    # ---------- 工具 ----------
    @staticmethod
    def _short(url: str, n: int = 90) -> str:
        return url if len(url) <= n else url[: n - 3] + "..."

    @staticmethod
    def _try_text(body: bytes):
        if body is None:
            return None
        try:
            return body.decode("utf-8", errors="replace")[:20000]
        except Exception:
            return None

    # ---------- 结束分析 ----------
    def analyze(self) -> dict:
        print("\n" + "=" * 64)
        print("抓取统计")
        print("=" * 64)
        print(f"  静态资源(落盘) : {self.count['assets']}")
        print(f"  接口响应(落盘) : {self.count['api']}")
        print(f"  请求失败       : {self.count['failed']}")
        print(f"  输出目录       : {self.store.out_dir}")

        suspects = []
        for r in self.api_records:
            text = (r.get("text") or "").lower()
            hit = [k for k in LEVEL_KEYS if k in text]
            if hit:
                suspects.append((r, hit))

        print("\n疑似「关卡清单」响应：")
        if not suspects:
            print("  (未识别到，请人工查看 _api/ 目录)")
        for r, hit in suspects:
            print(f"  - {r['path']}")
            print(f"      URL: {r['url']}")
            print(f"      命中关键字: {hit}")

        pattern = self.analyze_pull_pattern()
        summary = {
            "url": self.url,
            "game_id": self.game_id,
            "suspects": [{"path": r["path"], "url": r["url"], "keys": h} for r, h in suspects],
            "pull_pattern": pattern,
            "ws_sessions": len(self.ws_log),
        }
        sp = self.store.write_summary(summary)
        print(f"\n摘要已写入: {sp}")
        return pattern

    def analyze_pull_pattern(self) -> dict:
        """分析关卡资源是「一次性全拉」还是「一关一拉」。"""
        print("\n" + "=" * 64)
        print("关卡数据拉取模式分析")
        print("=" * 64)
        records = read_reqlog(self.store.reqlog_path)
        if not records:
            print("  无请求记录")
            return {"mode": "unknown"}

        def ts_of(rec):
            try:
                return datetime.fromisoformat(rec["ts"]).timestamp()
            except Exception:
                return None

        for r in records:
            r["_t"] = ts_of(r)

        level_pat = re_compile(
            r"(level|关卡|kaiguan|guanqia|/g\d+|关\d+|scene|chapter|"
            r"levelscript|levelconfig|getLevel|LevelScripts|Pool|Config)")
        level_hits = [r for r in records if r.get("url") and level_pat.search(r["url"])]
        api_hits = [r for r in records if r.get("ctype", "").startswith("application/json")
                    or "json" in (r.get("url") or "").lower()]

        BATCH_GAP = 4.0
        timed = [r for r in records if r.get("_t") is not None]
        timed.sort(key=lambda x: x["_t"])
        batches, cur, last_t = [], [], None
        for r in timed:
            if last_t is not None and (r["_t"] - last_t) > BATCH_GAP:
                if cur:
                    batches.append(cur)
                cur = []
            cur.append(r)
            last_t = r["_t"]
        if cur:
            batches.append(cur)

        api_pattern = re_compile(r"(getLevel|getKaiGuan|getLevelsConfig|level.*config)")
        batch_api = []
        for i, b in enumerate(batches):
            calls = [r["url"] for r in b if api_pattern.search(r.get("url", ""))]
            batch_api.append({
                "batch": i + 1,
                "start": datetime.fromtimestamp(b[0]["_t"]).strftime("%H:%M:%S"),
                "reqs": len(b),
                "api_calls": calls,
            })

        mode = "unknown"
        api_batches = [x for x in batch_api if x["api_calls"]]
        if len(api_batches) >= 2:
            mode = "per_level"
        elif len(api_batches) == 1:
            mode = "one_shot_or_by_first_batch" if api_batches[0]["batch"] == 1 else "single_batch"

        print(f"  总请求数        : {len(records)}")
        print(f"  疑似关卡相关请求: {len(level_hits)}")
        print(f"  JSON 响应数     : {len(api_hits)}")
        print(f"  切分出批次      : {len(batches)} 批（相邻间隔 > {BATCH_GAP}s 视为新批）")
        print("\n  各批次关卡接口调用：")
        for x in batch_api:
            apis = x["api_calls"] or ["（无）"]
            print(f"    批次{x['batch']} @ {x['start']}  请求{x['reqs']:>4}条  接口: {apis[:3]}")
        print(f"\n  >>> 判定模式: {mode}")
        if mode == "per_level":
            print("      => 一关一拉：必须【逐关点击】才能抓全")
        elif mode == "one_shot_or_by_first_batch":
            print("      => 疑似一次性：关卡清单在首批就全部拿到")
        else:
            print("      => 不明确，请人工核对 _api/ 目录里的关卡 JSON")

        return {
            "mode": mode,
            "total_reqs": len(records),
            "level_related": len(level_hits),
            "json_responses": len(api_hits),
            "batches": batch_api,
        }

    # ---------- 交互模式：先浏览、后抓取 ----------
    def run_interactive(self, proxy: str = "", mobile: bool = False):
        """打开浏览器先浏览（不记录），等 record_flag 后再开始记录，等 stop_flag 结束。

        与 run() 的区别：hook 全程挂着但内部按 self.recording 决定是否记录，
        因此「开始抓取」前的浏览流量不会被落盘。
        """
        self.store.prepare()
        print("=" * 64)
        print(f"起始URL : {self.url}")
        print(f"输出目录: {self.store.out_dir}")
        print("=" * 64)
        print("浏览器已打开。现在可以自由浏览去找到目标游戏（此阶段不记录）。")
        print("找到后回到网页界面点【开始抓取】，再进入游戏/关卡。\n")

        closed = threading.Event()
        launch_kwargs = {
            "headless": False,
            "args": ["--disable-http-cache", "--disable-application-cache"],
        }
        if proxy:
            launch_kwargs["proxy"] = {"server": proxy}

        with sync_playwright() as p:
            browser = p.chromium.launch(**launch_kwargs)
            ctx_kwargs = {
                "bypass_csp": True,
                "ignore_https_errors": True,
                "extra_http_headers": {"Cache-Control": "no-cache", "Pragma": "no-cache"},
            }
            if mobile:
                ctx_kwargs.update({
                    "user_agent": ("Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) "
                                   "AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 "
                                   "Mobile/15E148 Safari/604.1"),
                    "viewport": {"width": 414, "height": 896},
                    "device_scale_factor": 2,
                    "is_mobile": True,
                    "has_touch": True,
                })
            context = browser.new_context(**ctx_kwargs)
            page = context.new_page()

            def attach_hooks(pg):
                try:
                    pg.on("request", self.on_request)
                    pg.on("response", self.on_response)
                    pg.on("requestfailed", self.on_request_failed)
                    pg.on("console", self.on_console)
                    pg.on("pageerror", self.on_pageerror)
                    pg.on("websocket", self.on_websocket)
                except Exception as e:
                    print(f"hook 注册异常: {e}")

            attach_hooks(page)
            context.on("page", attach_hooks)

            context.on("close", lambda *_: closed.set())
            page.on("close", lambda *_: closed.set())

            page.goto(self.url, wait_until="domcontentloaded", timeout=60000)
            print(f">>> 起始页已加载: {page.url}")

            def tick(ms=250):
                try:
                    page.wait_for_timeout(ms)
                except Exception:
                    closed.set()
                    time.sleep(ms / 1000.0)
                if self.recording:
                    self.drain()

            # 阶段1：等待「开始抓取」（此期间不记录）
            while not self.record_flag.is_set() and not closed.is_set() \
                    and not self.stop_flag.is_set():
                tick()
            if closed.is_set():
                print(">>> 浏览器已关闭，结束。")

            if self.record_flag.is_set() and not self.stop_flag.is_set():
                self.recording = True
                print("\n>>> 【开始抓取】已开始记录。请进入游戏、点进关卡；完成后点【结束抓取】。\n")

            # 阶段2：记录中，等待「结束」
            while not self.stop_flag.is_set() and not closed.is_set():
                tick()

            # 收尾：读干净剩余队列
            for _ in range(30):
                self.drain()
                if not self.pending_responses:
                    break
                try:
                    page.wait_for_timeout(200)
                except Exception:
                    time.sleep(0.2)

            try:
                cookies = context.cookies()
                self.cookies_jar = "; ".join(f"{c['name']}={c['value']}" for c in cookies)
            except Exception:
                pass

            for closer in (context.close, browser.close):
                try:
                    closer()
                except Exception:
                    pass

        self.store.close()
        if self.ws_log:
            try:
                with open(self.store.out_dir + "/_wslog.json", "w", encoding="utf-8") as f:
                    json.dump(self.ws_log, f, ensure_ascii=False, indent=1)
            except Exception:
                pass
        return self.analyze()

    # ---------- 主流程 ----------
    def run(self, wait_sec: int = 0, proxy: str = "", mobile: bool = False):
        self.store.prepare()
        print("=" * 64)
        print(f"游戏URL : {self.url}")
        print(f"输出目录: {self.store.out_dir}")
        print(f"代理    : {proxy or '（直连）'}")
        print("=" * 64)
        print("浏览器已打开。请手动操作：进入游戏、点进每一关、触发需要加载的内容。")
        print("抓取全程进行中；操作完成后【关闭浏览器窗口】即自动结束。\n")

        closed = threading.Event()
        launch_kwargs = {
            "headless": False,
            "args": ["--disable-http-cache", "--disable-application-cache"],
        }
        if proxy:
            launch_kwargs["proxy"] = {"server": proxy}

        with sync_playwright() as p:
            browser = p.chromium.launch(**launch_kwargs)
            ctx_kwargs = {
                "bypass_csp": True,
                "ignore_https_errors": True,
                "extra_http_headers": {"Cache-Control": "no-cache", "Pragma": "no-cache"},
            }
            if mobile:
                ctx_kwargs.update({
                    "user_agent": ("Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) "
                                   "AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 "
                                   "Mobile/15E148 Safari/604.1"),
                    "viewport": {"width": 414, "height": 896},
                    "device_scale_factor": 2,
                    "is_mobile": True,
                    "has_touch": True,
                })
            context = browser.new_context(**ctx_kwargs)
            page = context.new_page()

            def attach_hooks(pg):
                try:
                    pg.on("request", self.on_request)
                    pg.on("response", self.on_response)
                    pg.on("requestfailed", self.on_request_failed)
                    pg.on("console", self.on_console)
                    pg.on("pageerror", self.on_pageerror)
                    pg.on("websocket", self.on_websocket)
                except Exception as e:
                    print(f"hook 注册异常: {e}")

            attach_hooks(page)
            context.on("page", attach_hooks)

            def on_close(*_args):
                closed.set()

            context.on("close", on_close)
            page.on("close", on_close)

            page.goto(self.url, wait_until="domcontentloaded", timeout=60000)
            print(f">>> 首页导航完成，当前地址: {page.url}")
            try:
                print(f">>> 页面标题: {page.title()[:80]}")
                body_len = page.evaluate("document.body ? document.body.innerText.length : 0")
                print(f">>> 页面可见文本长度: {body_len}（若为 0 说明白屏/未加载）")
            except Exception as e:
                print(f">>> 首页信息读取失败（页面可能正在跳转）: {e}")

            def tick(ms=250):
                try:
                    page.wait_for_timeout(ms)
                except Exception:
                    closed.set()
                    time.sleep(ms / 1000.0)
                self.drain()

            if wait_sec > 0:
                end_at = time.time() + wait_sec
                while (time.time() < end_at and not closed.is_set()
                       and not self.stop_flag.is_set()):
                    tick()
                if self.stop_flag.is_set():
                    print("\n>>> 收到「立即结束」信号，停止抓取。")
            else:
                print(">>> 关闭浏览器窗口即结束抓取（也可在本终端按 Ctrl+C）。\n")
                try:
                    while not closed.is_set() and not self.stop_flag.is_set():
                        tick()
                except KeyboardInterrupt:
                    print("\n（收到 Ctrl+C，结束抓取）")

            # 收尾：读干净剩余队列
            for _ in range(30):
                self.drain()
                if not self.pending_responses:
                    break
                try:
                    page.wait_for_timeout(200)
                except Exception:
                    time.sleep(0.2)

            # 保存 Cookie，供补全携带
            try:
                cookies = context.cookies()
                self.cookies_jar = "; ".join(f"{c['name']}={c['value']}" for c in cookies)
            except Exception:
                pass

            for closer in (context.close, browser.close):
                try:
                    closer()
                except Exception:
                    pass

        self.store.close()
        # 记录 WS 会话，供打包
        if self.ws_log:
            try:
                with open(self.store.out_dir + "/_wslog.json", "w", encoding="utf-8") as f:
                    json.dump(self.ws_log, f, ensure_ascii=False, indent=1)
            except Exception:
                pass
        return self.analyze()


def re_compile(pattern: str):
    import re
    flags = re.IGNORECASE
    # 复用 capture 内部常用的关卡正则
    return re.compile(pattern, flags)
