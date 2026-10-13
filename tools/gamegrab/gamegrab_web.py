# -*- coding: utf-8 -*-
"""gamegrab_web — gamegrab 抓取器的本地可视化界面（零第三方依赖）。

设计：
    * 后端用标准库 http.server（ThreadingHTTPServer），不引入 Flask。
    * 单个后台线程执行「抓取 → 归位 → 补全 → 打包」流水线，stdout 重定向到内存环形日志。
    * 前端为内联单页 HTML（本文件内），输入网址、实时看日志、打包下载。

用法：
    python gamegrab_web.py            # 默认 http://127.0.0.1:8770
    python gamegrab_web.py 9000       # 指定端口
"""

from __future__ import annotations

import io
import json
import os
import shutil
import sys
import threading
import traceback
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from ggrab.capture import Capture  # noqa: E402
from ggrab.complete import Completer  # noqa: E402
from ggrab.diagnose import diagnose  # noqa: E402
from ggrab.pack import pack_game  # noqa: E402
from ggrab.relocate import relocate  # noqa: E402
from ggrab.store import Store  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
OUT_ROOT = os.path.join(HERE, "grab_out")

# ============================================================
# 任务状态（单任务；界面一次跑一个）
# ============================================================

STATE_LOCK = threading.Lock()
STATE = {
    "running": False,
    "phase": "idle",          # idle | capture | relocate | complete | pack | done | error
    "percent": 0,
    "out_dir": "",
    "zip": "",
    "error": "",
    "started_at": "",
}


class _LogWriter(io.TextIOBase):
    """把流水线 stdout 写入内存环形缓冲区，供前端轮询。"""

    def __init__(self, buf: list, cap: int = 4000):
        self._buf = buf
        self._cap = cap

    def write(self, s: str) -> int:
        if not s:
            return 0
        for line in s.splitlines() or [""]:
            self._buf.append(line)
        if len(self._buf) > self._cap:
            del self._buf[: len(self._buf) - self._cap]
        return len(s)

    def flush(self):
        pass


LOG_BUF: list = []
#: 当前会话的 Capture 实例（供 /api/record、/api/stop 控制）
CURRENT_CAP = None
#: 当前会话的输出目录（供 /api/discard 删除）
CURRENT_OUT = ""
#: 丢弃标记：置位后，收尾阶段不做补全/打包，并删除输出目录
DISCARD_FLAG = threading.Event()


def _set(**kw):
    with STATE_LOCK:
        STATE.update(kw)


def _pipeline_after_capture(out_dir: str, cookies: str, name: str, game_id: str):
    """抓取结束后的公共收尾：归位 → 补全 → 打包 → 诊断。"""
    if DISCARD_FLAG.is_set():
        print("\n[web] 已丢弃本次数据，跳过补全与打包。")
        _set(phase="discarded", percent=0, zip="")
        return
    _set(phase="relocate", percent=45)
    print("\n[web] 归位静态资源 ...")
    relocate(out_dir)

    _set(phase="complete", percent=55)
    print("\n[web] 引擎识别 + 补全（含动态资源包二次补抓）...")
    comp = Completer(out_dir, cookies=cookies)
    comp.run()

    _set(phase="pack", percent=85)
    print("\n[web] 打包 ...")
    zip_path = pack_game(out_dir, name=name, game_id=game_id)

    _set(phase="done", percent=100, zip=zip_path or "")
    print(f"\n[web] 完成：{zip_path}")

    try:
        rep = diagnose(out_dir)
        with open(os.path.join(out_dir, "report.json"), "w", encoding="utf-8") as f:
            json.dump(rep, f, ensure_ascii=False, indent=2)
        print(f"[web] 诊断评分: {rep.get('playable_score')} / 100 "
              f"（动态包缺失 {len((rep.get('dynamic_bundles') or {}).get('missing') or [])}）")
    except Exception as e:
        print(f"[web] 诊断失败（不影响打包）: {e}")


def _run_session(url: str, out_dir: str, mobile: bool, name: str, game_id: str):
    """后台线程：打开浏览器 → 先浏览 → 等「开始抓取」→ 记录 → 等「结束」→ 收尾。

    与旧版区别：浏览器打开后不立即记录；用户浏览找到游戏、点【开始抓取】后才记录；
    不想要则可【丢弃】。整个过程 stdout 重定向为内存日志。
    """
    global CURRENT_CAP
    old_stdout = sys.stdout
    sys.stdout = _LogWriter(LOG_BUF)
    try:
        _set(running=True, phase="browsing", percent=0, error="", zip="",
             out_dir=out_dir, started_at=datetime.now().strftime("%H:%M:%S"))
        print(f"[web] 打开浏览器（浏览阶段，不记录）: {url}")
        print(f"[web] 输出目录: {out_dir}\n")

        store = Store(out_dir)
        cap = Capture(url, store, game_id)
        CURRENT_CAP = cap
        cap.run_interactive(proxy="", mobile=mobile)
        CURRENT_CAP = None

        _pipeline_after_capture(out_dir, cap.cookies_jar, name, game_id)
    except Exception as e:
        _set(phase="error", error=str(e))
        print(f"[web] 出错: {e}")
        traceback.print_exc()
    finally:
        _set(running=False)
        sys.stdout = old_stdout


# ============================================================
# HTTP 处理
# ============================================================

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass  # 静音默认访问日志

    def _json(self, obj, code=200):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _html(self, html: str):
        body = html.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        u = urlparse(self.path)
        if u.path in ("/", "/index.html"):
            return self._html(PAGE)
        if u.path == "/api/status":
            with STATE_LOCK:
                return self._json(dict(STATE))
        if u.path == "/api/log":
            since = int((parse_qs(u.query).get("since") or ["0"])[0])
            return self._json({"lines": LOG_BUF[since:], "next": len(LOG_BUF)})
        if u.path == "/api/download":
            return self._download()
        return self._json({"error": "not found"}, 404)

    def do_POST(self):
        u = urlparse(self.path)
        try:
            ln = int(self.headers.get("Content-Length") or 0)
            raw = self.rfile.read(ln) if ln else b"{}"
            data = json.loads(raw.decode("utf-8") or "{}")
        except Exception:
            data = {}

        if u.path == "/api/open":
            return self._open(data)
        if u.path == "/api/record":
            return self._record()
        if u.path == "/api/stop":
            return self._stop()
        if u.path == "/api/discard":
            return self._discard()
        if u.path == "/api/pack":
            return self._pack(data)
        if u.path == "/api/diagnose":
            return self._diagnose(data)
        return self._json({"error": "not found"}, 404)

    # ---------- 动作 ----------
    def _open(self, data):
        """打开浏览器进入浏览阶段（不记录）。"""
        global CURRENT_OUT
        with STATE_LOCK:
            if STATE["running"]:
                return self._json({"error": "已有会话在运行，请先结束或丢弃"}, 409)
        url = (data.get("url") or "").strip()
        if not url:
            return self._json({"error": "缺少 url"}, 400)
        mobile = bool(data.get("mobile"))
        name = (data.get("name") or "").strip()
        game_id = str(data.get("id") or "").strip()
        ts = datetime.now().strftime("%Y%m%d_%H%M%S")
        tag = f"game{game_id}_" if game_id else ""
        out_dir = os.path.join(OUT_ROOT, f"{tag}{ts}")
        CURRENT_OUT = out_dir
        DISCARD_FLAG.clear()
        LOG_BUF.clear()
        threading.Thread(
            target=_run_session,
            args=(url, out_dir, mobile, name, game_id),
            daemon=True,
        ).start()
        return self._json({"ok": True, "out_dir": out_dir})

    def _record(self):
        """在浏览阶段点「开始抓取」→ 从此刻开始记录。"""
        cap = CURRENT_CAP
        if cap is None:
            return self._json({"ok": False, "msg": "当前无会话"})
        cap.record_flag.set()
        _set(phase="capture", percent=20)
        return self._json({"ok": True, "msg": "已开始记录，请进入游戏/关卡"})

    def _stop(self):
        """结束抓取 → 收尾（补全 + 打包）。"""
        cap = CURRENT_CAP
        if cap is None:
            return self._json({"ok": False, "msg": "当前无进行中的抓取"})
        try:
            cap.record_flag.set()   # 防止仍在浏览阶段就直接结束
            cap.stop_flag.set()
        except Exception as e:
            return self._json({"error": str(e)}, 500)
        return self._json({"ok": True, "msg": "已发送结束信号，正在收尾并补全打包"})

    def _discard(self):
        """丢弃本次数据：结束会话并删除输出目录（不补全、不打包）。"""
        global CURRENT_OUT
        DISCARD_FLAG.set()
        cap = CURRENT_CAP
        if cap is not None:
            try:
                cap.record_flag.set()
                cap.stop_flag.set()
            except Exception:
                pass
        # 同步删除输出目录（若进程仍在收尾，其写盘可能失败，但目录会被清）
        try:
            if CURRENT_OUT and os.path.isdir(CURRENT_OUT):
                shutil.rmtree(CURRENT_OUT, ignore_errors=True)
        except Exception:
            pass
        CURRENT_OUT = ""
        _set(phase="discarded", percent=0, zip="")
        return self._json({"ok": True, "msg": "已丢弃本次数据并删除目录"})

    def _pack(self, data):
        with STATE_LOCK:
            if STATE["running"]:
                return self._json({"error": "任务运行中，稍后再打包"}, 409)
        store_dir = (data.get("out_dir") or STATE.get("out_dir") or "").strip()
        if not store_dir or not os.path.isdir(store_dir):
            return self._json({"error": "目录不存在"}, 400)
        try:
            zip_path = pack_game(store_dir, name=(data.get("name") or ""),
                                 game_id=str(data.get("id") or ""))
            _set(zip=zip_path or "")
            return self._json({"ok": True, "zip": zip_path})
        except Exception as e:
            return self._json({"error": str(e)}, 500)

    def _diagnose(self, data):
        store_dir = (data.get("out_dir") or STATE.get("out_dir") or "").strip()
        if not store_dir or not os.path.isdir(store_dir):
            return self._json({"error": "目录不存在"}, 400)
        try:
            return self._json(diagnose(store_dir))
        except Exception as e:
            return self._json({"error": str(e)}, 500)

    def _download(self):
        with STATE_LOCK:
            zp = STATE.get("zip") or ""
        if not zp or not os.path.isfile(zp):
            return self._json({"error": "暂无可下载的 zip"}, 404)
        with open(zp, "rb") as f:
            body = f.read()
        fn = os.path.basename(zp)
        self.send_response(200)
        self.send_header("Content-Type", "application/zip")
        self.send_header("Content-Disposition", f'attachment; filename="{fn}"')
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


PAGE = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<title>gamegrab 抓取器</title>
<style>
  * { box-sizing: border-box; }
  body { margin:0; font:14px/1.6 system-ui,"Microsoft YaHei",sans-serif; background:#0f1115; color:#e6e6e6; }
  header { padding:14px 20px; background:#161a22; border-bottom:1px solid #262b36; font-size:16px; font-weight:600; }
  .wrap { max-width:1080px; margin:0 auto; padding:18px 20px 60px; }
  .row { display:flex; gap:10px; flex-wrap:wrap; align-items:center; }
  input[type=text], input[type=number] { background:#0b0d11; border:1px solid #2b3240; color:#e6e6e6; padding:9px 11px; border-radius:7px; }
  input[type=text] { flex:1; min-width:280px; }
  button { background:#3b82f6; color:#fff; border:0; padding:9px 16px; border-radius:7px; cursor:pointer; font-size:14px; }
  button.ghost { background:#232a36; }
  button:disabled { opacity:.45; cursor:not-allowed; }
  .card { background:#131722; border:1px solid #222833; border-radius:10px; padding:14px 16px; margin-top:14px; }
  .bar { height:8px; background:#1d2330; border-radius:5px; overflow:hidden; margin-top:8px; }
  .bar > i { display:block; height:100%; width:0; background:linear-gradient(90deg,#3b82f6,#22c55e); transition:width .3s; }
  pre.log { background:#0a0c10; border:1px solid #1e2430; border-radius:8px; padding:10px; height:340px; overflow:auto; font:12px/1.5 Consolas,monospace; white-space:pre-wrap; word-break:break-all; }
  .kv { display:flex; gap:22px; flex-wrap:wrap; }
  .kv b { color:#9fb3c8; font-weight:500; }
  .warn { color:#fbbf24; }
  .ok { color:#22c55e; }
  .err { color:#f87171; }
  label { color:#9fb3c8; }
</style>
</head>
<body>
<header>🎮 gamegrab 抓取器 · 可视化</header>
<div class="wrap">
  <div class="card">
    <div class="row">
      <input type="text" id="url" placeholder="网址（导航站或游戏页均可）"/>
      <label><input type="checkbox" id="mobile"/> 手机UA</label>
      <button id="open">打开浏览器</button>
      <button id="record" disabled>开始抓取</button>
      <button id="stop" class="ghost" disabled>结束抓取</button>
      <button id="discard" class="ghost" disabled>丢弃</button>
    </div>
    <div class="row" style="margin-top:10px">
      <input type="text" id="name" placeholder="游戏名（打包用，可留空）" style="max-width:260px"/>
      <input type="text" id="gid" placeholder="游戏ID（可留空）" style="max-width:160px"/>
    </div>
    <div class="bar"><i id="pbar"></i></div>
  </div>

  <div class="card">
    <div class="kv">
      <span><b>阶段</b> <span id="phase">idle</span></span>
      <span><b>进度</b> <span id="percent">0</span>%</span>
      <span><b>输出目录</b> <span id="outdir" style="font-size:12px"></span></span>
    </div>
    <div class="row" style="margin-top:10px">
      <button class="ghost" id="diag">查看诊断</button>
      <button class="ghost" id="pack">重新打包</button>
      <button id="dl" disabled>下载 zip</button>
    </div>
    <pre class="log" id="log"></pre>
  </div>

  <div class="card" id="dyncard" style="display:none">
    <div id="dyn"></div>
  </div>
</div>

<script>
const $ = (id) => document.getElementById(id);
let logCursor = 0, timer = null;

async function post(path, body) {
  const r = await fetch(path, {method:"POST", headers:{"Content-Type":"application/json"},
                               body: JSON.stringify(body||{})});
  return r.json();
}

async function poll() {
  try {
    const st = await (await fetch("/api/status")).json();
    $("phase").textContent = st.phase;
    $("percent").textContent = st.percent;
    $("pbar").style.width = st.percent + "%";
    $("outdir").textContent = st.out_dir || "";
    $("open").disabled = st.running;
    $("record").disabled = !(st.running && st.phase === "browsing");
    $("stop").disabled = !(st.running && (st.phase === "capture" || st.phase === "browsing"));
    $("discard").disabled = !st.running;
    $("pack").disabled = st.running;
    if (st.zip) { $("dl").disabled = false; } else { $("dl").disabled = true; }
    if (st.error) { $("phase").innerHTML = '<span class="err">'+st.phase+'</span>'; }
    const lg = await (await fetch("/api/log?since="+logCursor)).json();
    if (lg.lines && lg.lines.length) {
      const el = $("log");
      el.textContent += lg.lines.join("\\n") + "\\n";
      logCursor = lg.next;
      el.scrollTop = el.scrollHeight;
    }
    if (!st.running && (st.phase === "done" || st.phase === "discarded" || st.phase === "error")) {
      clearInterval(timer); timer = null;
      if (st.phase === "done") { loadDiag(); }
    }
  } catch (e) {}
}

function beginPolling() {
  if (timer) return;
  timer = setInterval(poll, 1200);
}

$("open").onclick = async () => {
  const url = $("url").value.trim();
  if (!url) { alert("请输入网址"); return; }
  $("log").textContent = ""; logCursor = 0;
  $("dyncard").style.display = "none";
  const r = await post("/api/open", {
    url, mobile: $("mobile").checked,
    name: $("name").value.trim(), id: $("gid").value.trim()
  });
  if (r.error) { alert(r.error); return; }
  $("log").textContent = "[web] 浏览器已打开。请浏览找到目标游戏，然后点【开始抓取】。\\n";
  beginPolling();
};

$("record").onclick = async () => {
  const r = await post("/api/record", {});
  if (r.error) { alert(r.error); return; }
  $("log").textContent += "[web] " + (r.msg||"") + "\\n";
};

$("stop").onclick = async () => {
  const r = await post("/api/stop", {});
  if (r.error) { alert(r.error); return; }
  if (r.msg) { $("log").textContent += "[web] " + r.msg + "\\n"; }
  $("dl").disabled = true;
};

$("discard").onclick = async () => {
  if (!confirm("确定丢弃本次抓取的数据？将删除已抓目录。")) { return; }
  const r = await post("/api/discard", {});
  if (r.error) { alert(r.error); return; }
  $("log").textContent += "[web] " + (r.msg||"") + "\\n";
  $("dl").disabled = true;
  $("dyncard").style.display = "none";
};

$("pack").onclick = async () => {
  const r = await post("/api/pack", {name:$("name").value.trim(), id:$("gid").value.trim()});
  if (r.error) { alert(r.error); return; }
  alert("已重新打包：" + r.zip);
  $("dl").disabled = false;
};

$("diag").onclick = loadDiag;
$("dl").onclick = () => { window.location = "/api/download"; };

async function loadDiag() {
  const r = await post("/api/diagnose", {});
  if (r.error) { return; }
  const dyn = r.dynamic_bundles || {};
  const miss = dyn.missing || [];
  let h = '<div class="kv"><span><b>引擎</b> '+r.engine+'</span>'
        + '<span><b>可玩性评分</b> <span class="'+(r.playable_score>=80?'ok':'warn')+'">'
        + r.playable_score+' / 100</span></span>'
        + '<span><b>缺失资源</b> '+r.missing_count+'</span>'
        + '<span><b>动态包候选</b> '+dyn.detected+'（已抓 '+(dyn.present||0)+'，缺失 '+miss.length+'）</span></div>';
  if (miss.length) {
    h += '<div class="warn" style="margin-top:10px">⚠️ 检测到运行时动态资源包缺失，'
       + '打包前请确保已完成「补全」（含二次补抓）：</div>'
       + '<div style="font:12px Consolas;color:#cbd5e1;max-height:180px;overflow:auto;margin-top:6px">'
       + miss.slice(0,80).join("、") + (miss.length>80?(' … 共 '+miss.length+' 个'):'') + '</div>';
  } else if (dyn.detected) {
    h += '<div class="ok" style="margin-top:8px">✔ 动态资源包已齐全</div>';
  }
  $("dyn").innerHTML = h;
  $("dyncard").style.display = "block";
}
</script>
</body>
</html>"""


def main():
    port = 8770
    if len(sys.argv) > 1:
        try:
            port = int(sys.argv[1])
        except Exception:
            pass
    srv = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"gamegrab 可视化界面已启动： http://127.0.0.1:{port}")
    print("（Ctrl+C 退出）")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        print("\n已退出")


if __name__ == "__main__":
    main()
