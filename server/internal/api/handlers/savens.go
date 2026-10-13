package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// saveNamespaceScript 游戏页命名空间代理脚本。
//
// 目的：H5 小游戏的 localStorage / IndexedDB 落点没有统一约定，无法归属到具体游戏。
// 通过给每个游戏页注入本脚本，在**当前文档 realm** 内对 Storage / IDBFactory 原型方法
// 统一加游戏前缀，使「H5 存档按游戏归属」成为可能（导出/隔离/清理的前提）。
//
// 约束：必须用 Go 原样字符串承载，脚本内**不得出现反引号与 Go 模板占位**。
//
// 另含「全屏守卫」：部分 H5 游戏（如 Cocos 系）会在首次触摸时自动全屏，
// 全屏后画面逃出 PlayView 的竖屏外框，观感上像是「突然变成横屏」。
// 这里统一禁用页面内的全屏请求，全屏改由外层（PlayView 工具栏 / 悬浮球）控制。
//
// 另含「倍速」：向页面暴露 window.__uogameSetSpeed(rate)，由外层（悬浮球）调用，
// 通过放大 rAF 时间戳 / performance.now / Date.now 的「时间流速」来整体加速游戏逻辑。
const saveNamespaceScript = `(function () {
  try {
    var el = document.currentScript;
    var ns = el && el.getAttribute ? el.getAttribute('data-ns') : '';
    if (!ns) return;
    var LP = ns + ':';    // localStorage 前缀
    var DP = ns + '::';   // IndexedDB 库名前缀

    // ---------- localStorage ----------
    var SP = window.Storage && window.Storage.prototype;
    if (SP && !SP.__uogameNS) {
      SP.__uogameNS = ns;
      var gGet = SP.getItem, gSet = SP.setItem, gRemove = SP.removeItem, gKey = SP.key, gClear = SP.clear;
      var gLen = Object.getOwnPropertyDescriptor(SP, 'length');

      SP.getItem = function (k) {
        k = String(k);
        var v = gGet.call(this, LP + k);
        if (v === null || v === undefined) {
          // 老存档兼容：无前缀的旧键仍可读，并一次性迁移到新前缀键（非破坏式，不删旧键）
          var old = gGet.call(this, k);
          if (old !== null && old !== undefined) {
            try { gSet.call(this, LP + k, old); } catch (e) {}
            return old;
          }
        }
        return v;
      };
      SP.setItem = function (k, v) { return gSet.call(this, LP + String(k), v); };
      SP.removeItem = function (k) { return gRemove.call(this, LP + String(k)); };

      var ownKeys = function (store) {
        var out = [], n = gLen ? gLen.get.call(store) : 0;
        for (var i = 0; i < n; i++) {
          var rk = gKey.call(store, i);
          if (rk && rk.indexOf(LP) === 0) out.push(rk);
        }
        return out;
      };

      SP.key = function (i) {
        var all = ownKeys(this);
        i = Number(i);
        if (!(i >= 0) || i >= all.length) return null;
        return all[i].slice(LP.length);
      };
      SP.clear = function () {
        var all = ownKeys(this);
        for (var i = 0; i < all.length; i++) gRemove.call(this, all[i]);
      };
      if (gLen && gLen.get && gLen.configurable !== false) {
        try {
          Object.defineProperty(SP, 'length', {
            configurable: true,
            get: function () { return ownKeys(this).length; }
          });
        } catch (e) {}
      }
    }

    // ---------- IndexedDB ----------
    var IF = window.IDBFactory && window.IDBFactory.prototype;
    if (IF && !IF.__uogameNS) {
      IF.__uogameNS = ns;
      var dOpen = IF.open, dDel = IF.deleteDatabase;
      IF.open = function (name, ver) {
        if (arguments.length > 1) return dOpen.call(this, DP + String(name), ver);
        return dOpen.call(this, DP + String(name));
      };
      IF.deleteDatabase = function (name) { return dDel.call(this, DP + String(name)); };
      if (typeof IF.databases === 'function') {
        var dList = IF.databases;
        IF.databases = function () {
          return dList.call(this).then(function (list) {
            var out = [];
            (list || []).forEach(function (d) {
              if (d && typeof d.name === 'string' && d.name.indexOf(DP) === 0) {
                out.push({ name: d.name.slice(DP.length), version: d.version });
              }
            });
            return out;
          });
        };
      }
    }
  } catch (e) {
    try { console.warn('[savens] namespace init failed:', e); } catch (_) {}
  }
})();

// ---------- 全屏守卫（与命名空间无关，独立执行） ----------
(function () {
  // 1) 拦截页面内元素的全屏请求（各浏览器前缀统一置为拒绝）
  try {
    var noop = function () { return Promise.reject(new Error('uogame: fullscreen guarded')); };
    var P = window.Element && Element.prototype;
    if (P) {
      P.requestFullscreen = noop;
      P.webkitRequestFullscreen = noop;
      P.webkitRequestFullScreen = noop;
      P.mozRequestFullScreen = noop;
      P.msRequestFullscreen = noop;
    }
  } catch (e) {}

  // 2) 关闭 Cocos 的自动全屏（引擎异步加载，轮询至就绪，最多 10 秒）
  try {
    var tries = 0;
    var timer = setInterval(function () {
      tries += 1;
      var cc = window.cc;
      if (cc && cc.view && typeof cc.view.enableAutoFullScreen === 'function') {
        try { cc.view.enableAutoFullScreen(false); } catch (e) {}
        clearInterval(timer);
        return;
      }
      if (tries >= 100) clearInterval(timer);
    }, 100);
  } catch (e) {}
})();

// ---------- 倍速（H5 游戏调速，供外层调用） ----------
// 原理：多数 H5 引擎（Cocos / Laya / Egret / Pixi / Phaser）主循环都基于
// requestAnimationFrame 的时间戳或 performance.now()/Date.now() 计算每帧 dt。
// 这里把「时间流速」按倍率放大，使游戏逻辑整体加速。
// 注意：仅覆盖基于 rAF / 上述时间源的引擎；少数用 setTimeout 自驱的游戏不保证生效。
(function () {
  if (window.__uogameSpeedInstalled) return;
  window.__uogameSpeedInstalled = true;

  var rafNative = window.requestAnimationFrame || function (cb) { return setTimeout(function () { cb(Date.now()); }, 16); };
  var cafNative = window.cancelAnimationFrame || window.clearTimeout;
  var perfNow = (window.performance && performance.now) ? performance.now.bind(performance) : function () { return Date.now(); };
  var hasPerf = !!(window.performance && performance.now);

  var rate = 1;
  var baseReal = perfNow();
  var baseVirt = baseReal;

  function virtNow() { return baseVirt + (perfNow() - baseReal) * rate; }

  window.requestAnimationFrame = function (cb) {
    return rafNative.call(window, function () { return cb(virtNow()); });
  };
  window.cancelAnimationFrame = function (id) { return cafNative.call(window, id); };

  if (hasPerf) {
    try { window.performance.now = function () { return virtNow(); }; } catch (e) {}
  }
  Date.now = function () { return Math.round(virtNow()); };

  window.__uogameSetSpeed = function (s) {
    s = Number(s);
    if (!(s > 0)) s = 1;
    if (s < 0.25) s = 0.25;
    if (s > 8) s = 8;
    // 先按旧倍率结算当前虚拟时间，再重置基准，避免时间跳变
    baseVirt = virtNow();
    baseReal = perfNow();
    rate = s;
    try { console.log('[uogame] playback speed = ' + s + 'x'); } catch (e) {}
    return rate;
  };
})();
`

// ServeSaveNamespaceScript 输出命名空间代理脚本（公开静态资源，无敏感信息）
// GET /assets/savens-ns.js
func ServeSaveNamespaceScript(c *gin.Context) {
	c.Header("Content-Type", "text/javascript; charset=utf-8")
	c.Header("X-Content-Type-Options", "nosniff")
	// 脚本会随版本更新（如新增倍速能力），必须每次重校验，避免浏览器长期沿用旧副本
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.String(http.StatusOK, saveNamespaceScript)
}

// injectSaveNamespace 在 HTML 中插入命名空间脚本标签：
// 优先紧跟 <head...> 之后；无 head 则退到 <html...> 之后；再无则置于首个 <script> 之前；最后兜底置顶。
// 插入点必须早于游戏自身脚本，才能保证 patch 先生效。
func injectSaveNamespace(html, ns string) string {
	if ns == "" || html == "" {
		return html
	}
	// ?v=2：历史副本曾被 max-age 长期缓存，提升版本号以确保旧缓存被绕过
	tag := `<script src="/assets/savens-ns.js?v=2" data-ns="` + ns + `"></script>`
	lower := strings.ToLower(html)

	insertAfterTag := func(openTag string) (string, bool) {
		i := strings.Index(lower, openTag)
		if i < 0 {
			return "", false
		}
		j := strings.Index(lower[i:], ">")
		if j < 0 {
			return "", false
		}
		pos := i + j + 1
		return html[:pos] + tag + html[pos:], true
	}

	if out, ok := insertAfterTag("<head"); ok {
		return out
	}
	if out, ok := insertAfterTag("<html"); ok {
		return out
	}
	if i := strings.Index(lower, "<script"); i >= 0 {
		return html[:i] + tag + html[i:]
	}
	return tag + html
}