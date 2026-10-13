# -*- coding: utf-8 -*-
"""shim — 生成 XHR / fetch / WebSocket 三合一离线补丁脚本。

注入到 index.html <head> 最前，把三类请求重定向到本地快照：
    XHR        -> _apimap.json（URL path+query -> 本地文件）
    fetch      -> 同上
    WebSocket  -> _wsmap.json（URL -> 抓取到的收发帧回放）
未命中映射的写类/上报类请求：返回本地空成功，避免网络错误阻塞游戏。
"""

from __future__ import annotations

import json

_TEMPLATE = r"""
<script>
/* gamegrab 离线补丁：XHR + fetch + WebSocket 三合一重定向到本地快照 */
(function () {
  var API_HOST = __API_HOST__;
  var API_MAP = __API_MAP__;
  var WS_MAP = __WS_MAP__;
  window.__DBG_APIHOST = API_HOST;
  window.__DBG_APIMAP = API_MAP;
  window.__DBG_localFor = function (u) { try { return localFor(u); } catch (e) { return "ERR:" + e; } };

  function basePath() {
    var p = location.pathname;
    return p.substring(0, p.lastIndexOf('/') + 1);
  }
  function keyOf(url) {
    return String(url || "").replace(/^https?:\/\/[^/]+/, "");
  }
  function isCrossHost(url) {
    if (!API_HOST) return false;
    return String(url || "").indexOf(API_HOST) >= 0;
  }
  function localFor(url) {
    var k = keyOf(url);
    if (API_MAP && API_MAP[k]) return basePath() + API_MAP[k];
    // 退一步：忽略 query 再匹配
    var noq = k.split("?")[0];
    if (API_MAP && API_MAP[noq]) return basePath() + API_MAP[noq];
    // 再退一步：运行时 URL 可能未编码（如中文 senceId），而映射键是编码后的，
    // 故对 query 做一次「解码/编码」双向归一化后再匹配
    var cands = [normQuery(k, false), normQuery(k, true)];
    for (var i = 0; i < cands.length; i++) {
      if (cands[i] && API_MAP && API_MAP[cands[i]]) return basePath() + API_MAP[cands[i]];
      var cq = cands[i] ? cands[i].split("?")[0] : "";
      if (cq && API_MAP && API_MAP[cq]) return basePath() + API_MAP[cq];
    }
    return null;
  }
  /* 归一化 query：decode=true 时先解码再编码（统一为 %XX），decode=false 时先编码再解码（统一为原字符） */
  function normQuery(u, decode) {
    var i = u.indexOf("?");
    if (i < 0) return u;
    var path = u.slice(0, i), q = u.slice(i + 1);
    try {
      q = decode ? encodeURIComponent(decodeURIComponent(q)).replace(/%3D/g, "=").replace(/%26/g, "&") : decodeURIComponent(q);
    } catch (e) { return u; }
    return path + "?" + q;
  }

  /* 未命中映射的跨域请求：本地合成空成功响应，绝不外发（外网不可达会挂起卡死） */
  var MOCK_BODY = JSON.stringify({ success: true, code: 200, msg: "success", data: {} });

  /* ---------------- XHR ---------------- */
  var OrigXHR = window.XMLHttpRequest;
  function PatchedXHR() {
    var real = new OrigXHR();
    var self = this;
    var mode = "pass";     // pass | local | mock
    var target = "";
    this.open = function (method, url) {
      var cross = isCrossHost(url);
      if (cross) {
        var hit = localFor(url);
        if (hit) { mode = "local"; target = hit; }
        else { mode = "mock"; }
      } else {
        mode = "pass";
      }
      if (mode === "pass") return real.open.apply(real, arguments);
      return real.open(method, mode === "local" ? target : ("data:application/json;charset=utf-8," + encodeURIComponent(MOCK_BODY)));
    };
    this.setRequestHeader = function () { return real.setRequestHeader.apply(real, arguments); };
    this.send = function (body) {
      if (mode !== "pass") { try { body = undefined; } catch (e) {} }
      return real.send(body);
    };
    this.abort = function () { return real.abort(); };
    ["status","statusText","responseText","response","responseType","readyState",
     "timeout","withCredentials","upload"].forEach(function (k) {
      Object.defineProperty(self, k, {
        get: function () { return real[k]; },
        set: function (v) { try { real[k] = v; } catch (e) {} }
      });
    });
    ["onreadystatechange","onload","onerror","ontimeout","onloadend",
     "onprogress","onabort"].forEach(function (k) {
      Object.defineProperty(self, k, {
        get: function () { return real[k]; },
        set: function (v) { real[k] = v; }
      });
    });
    this.getAllResponseHeaders = function () { return real.getAllResponseHeaders(); };
    this.getResponseHeader = function (n) { return real.getResponseHeader(n); };
  }
  window.XMLHttpRequest = function () { return new PatchedXHR(); };
  /* 关键：补回 XHR 的静态常量。
     游戏代码普遍使用 XMLHttpRequest.DONE（=4）等常量做判断；
     替换构造器后这些静态属性会丢失，导致 `readyState === XMLHttpRequest.DONE`
     恒为 false，回调永不触发（表现为卡在加载页）。 */
  window.XMLHttpRequest.UNSENT = 0;
  window.XMLHttpRequest.OPENED = 1;
  window.XMLHttpRequest.HEADERS_RECEIVED = 2;
  window.XMLHttpRequest.LOADING = 3;
  window.XMLHttpRequest.DONE = 4;
  window.XMLHttpRequest.prototype = OrigXHR.prototype;

  /* ---------------- fetch ---------------- */
  if (window.fetch) {
    var origFetch = window.fetch.bind(window);
    window.fetch = function (input, init) {
      var url = (typeof input === "string") ? input : (input && input.url);
      if (isCrossHost(url)) {
        var hit = localFor(url);
        if (hit) return origFetch(hit, Object.assign({}, init, {method: "GET", body: undefined}));
        return Promise.resolve(new Response(MOCK_BODY, {
          status: 200, headers: {"Content-Type": "application/json"}
        }));
      }
      return origFetch(input, init);
    };
  }

  /* ---------------- WebSocket ---------------- */
  var OrigWS = window.WebSocket;
  window.WebSocket = function (url, protocols) {
    var sess = WS_MAP && WS_MAP[url];
    if (!sess) { return new OrigWS(url, protocols); }
    var self = this;
    this.url = url;
    this.readyState = 1; /* OPEN */
    this.binaryType = "arraybuffer";
    this.bufferedAmount = 0;
    this.extensions = "";
    this.protocol = (protocols && protocols[0]) || "";
    this.onopen = null; this.onmessage = null; this.onerror = null; this.onclose = null;
    var recv = (sess.recv || []).slice();
    function dispatch() {
      if (typeof self.onopen === "function") self.onopen({type: "open"});
      recv.forEach(function (msg, i) {
        setTimeout(function () {
          if (typeof self.onmessage === "function") self.onmessage({type: "message", data: msg});
          if (typeof self.addEventListener === "function") self.__listeners.forEach(function (f) {
            f({type: "message", data: msg});
          });
        }, 10 * (i + 1));
      });
      setTimeout(function () {
        if (typeof self.onclose === "function") self.onclose({type: "close", code: 1000});
      }, 10 * (recv.length + 1));
    }
    this.send = function (data) {
      /* 记录但不外发；如有 mock 回包可按需扩展 */
    };
    this.close = function () {
      this.readyState = 3;
      if (typeof this.onclose === "function") this.onclose({type: "close", code: 1000});
    };
    this.__listeners = [];
    this.addEventListener = function (t, f) { if (t === "message") this.__listeners.push(f); };
    this.removeEventListener = function () {};
    setTimeout(dispatch, 0);
    return this;
  };
  window.WebSocket.CONNECTING = 0;
  window.WebSocket.OPEN = 1;
  window.WebSocket.CLOSING = 2;
  window.WebSocket.CLOSED = 3;
})();
</script>
"""


def build_shim(api_host: str, api_map: dict, ws_map: dict | None = None) -> str:
    """生成离线补丁 <script> 文本（映射内联，避免异步竞态）。"""
    return (_TEMPLATE
            .replace("__API_HOST__", json.dumps(api_host or ""))
            .replace("__API_MAP__", json.dumps(api_map or {}, ensure_ascii=False))
            .replace("__WS_MAP__", json.dumps(ws_map or {}, ensure_ascii=False)))
