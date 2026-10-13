import { useEffect, useState } from 'react'

// 轻量路由：基于浏览器 History API（不依赖 react-router）
// 路由契约：
//   /game/:id   → { name: 'game', id }   游戏详情页（独立页面，脱离 Layout）
//   其它         → { name: 'app' }         应用主界面（内部仍由状态驱动切换区段）
//
// 说明：后端（_srv.exe）对未知路径已做 catch-all 回退到 SPA index.html，
// 且前端静态资源使用根绝对路径（/assets/...），故采用 history 风格路由可支持深链接与刷新。

// 解析当前地址为路由对象
export function parseRoute() {
  const path = typeof window !== 'undefined' ? window.location.pathname : '/'
  const m = path.match(/^\/game\/([^/]+)\/?$/)
  if (m) {
    return { name: 'game', id: decodeURIComponent(m[1]) }
  }
  return { name: 'app' }
}

// SPA 内部导航标记：pushState 写入 state，用于判断"历史记录是否由本 SPA 产生"
const SPA_STATE = { _spa: true }

// 导航：push/replace 到目标路径并广播（history.pushState 不触发 popstate，需手动同步）
export function navigate(path, { replace = false } = {}) {
  if (typeof window === 'undefined') return
  const cur = window.location.pathname + window.location.search
  if (cur === path) return
  if (replace) window.history.replaceState(SPA_STATE, '', path)
  else window.history.pushState(SPA_STATE, '', path)
  // 通知订阅者重新解析
  window.dispatchEvent(new PopStateEvent('popstate'))
}

// 当前历史记录是否由本 SPA 内部导航产生（深链接/直接打开时为 false）
export function isInternalHistory() {
  if (typeof window === 'undefined') return false
  return !!(window.history.state && window.history.state._spa)
}

// 订阅路由变化：返回当前路由对象
export function useRoute() {
  const [route, setRoute] = useState(parseRoute)
  useEffect(() => {
    const onPop = () => setRoute(parseRoute())
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])
  return route
}
