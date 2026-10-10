import { useCallback, useEffect, useRef, useState } from 'react'

// WebSocket 聊天连接：自动重连 + 消息回调
// 契约：ws(s)://<host>/api/ws/chat （鉴权通过 Cookie 或 ?token=）
export function useWebSocket(onMessage) {
  const [connected, setConnected] = useState(false)
  const wsRef = useRef(null)
  const retryRef = useRef(0)
  const timerRef = useRef(null)
  const cbRef = useRef(onMessage)
  cbRef.current = onMessage

  const connect = useCallback(() => {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const token = localStorage.getItem('uogame_token') || ''
    const url = `${proto}://${location.host}/api/ws/chat${
      token ? `?token=${encodeURIComponent(token)}` : ''
    }`
    let ws
    try {
      ws = new WebSocket(url)
    } catch {
      return
    }
    wsRef.current = ws

    ws.onopen = () => {
      retryRef.current = 0
      setConnected(true)
    }
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data)
        cbRef.current && cbRef.current(msg)
      } catch {
        /* ignore */
      }
    }
    ws.onclose = () => {
      setConnected(false)
      // 指数退避重连，上限 15s
      const delay = Math.min(1000 * 2 ** retryRef.current, 15000)
      retryRef.current += 1
      timerRef.current = setTimeout(connect, delay)
    }
    ws.onerror = () => {
      try {
        ws.close()
      } catch {
        /* ignore */
      }
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
      if (wsRef.current) {
        wsRef.current.onclose = null
        try {
          wsRef.current.close()
        } catch {
          /* ignore */
        }
      }
    }
  }, [connect])

  const send = useCallback((content) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ content }))
      return true
    }
    return false
  }, [])

  return { connected, send }
}
