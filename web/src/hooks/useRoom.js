import { useCallback, useEffect, useRef, useState } from 'react'
import { API } from '../api/endpoints'
import { getToken } from '../api/client'

// 房间 WebSocket 连接（对标 useWebSocket，但携带房间号与可选密码）
// 事件协议（与后端 room.go 对齐）：
//   服务端 → 客户端：joined / members / member_joined / member_left /
//                    owner_transferred / kicked / room_closed / chat / input / error
//   客户端 → 服务端：{ type: 'chat'|'input'|'kick'|'transfer'|'close', ... }
//
// 参数：
//   roomId   - 房间号（为空则不连接）
//   password - 房间密码（可选，经 ?password= 传入）
//   onEvent  - 事件回调 (evt) => void
export function useRoom(roomId, password, onEvent) {
  const [connected, setConnected] = useState(false)
  const [selfId, setSelfId] = useState(null)
  const [members, setMembers] = useState([])
  const [room, setRoom] = useState(null)
  const [error, setError] = useState('')
  const wsRef = useRef(null)
  const cbRef = useRef(onEvent)
  cbRef.current = onEvent

  const handle = useCallback((evt) => {
    switch (evt.type) {
      case 'joined':
        setRoom(evt.room || null)
        setMembers((evt.room && evt.room.members) || [])
        setSelfId(evt.self_id ?? null)
        break
      case 'members':
        setMembers(evt.members || [])
        break
      case 'error':
        setError(evt.message || '房间错误')
        break
      default:
        break
    }
    cbRef.current && cbRef.current(evt)
  }, [])

  useEffect(() => {
    if (!roomId) return undefined
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const token = getToken() || ''
    const qs = new URLSearchParams()
    if (token) qs.set('token', token)
    if (password) qs.set('password', password)
    const url = `${proto}://${location.host}${API.wsRoom(roomId)}?${qs.toString()}`

    let ws
    try {
      ws = new WebSocket(url)
    } catch {
      setError('无法建立房间连接')
      return undefined
    }
    wsRef.current = ws

    ws.onopen = () => setConnected(true)
    ws.onmessage = (ev) => {
      try {
        handle(JSON.parse(ev.data))
      } catch {
        /* ignore */
      }
    }
    ws.onclose = () => setConnected(false)
    ws.onerror = () => {
      try {
        ws.close()
      } catch {
        /* ignore */
      }
    }

    return () => {
      if (wsRef.current) {
        wsRef.current.onclose = null
        try {
          wsRef.current.close()
        } catch {
          /* ignore */
        }
      }
      wsRef.current = null
      setConnected(false)
    }
  }, [roomId, password, handle])

  // 发送原始事件对象
  const sendEvent = useCallback((payload) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(payload))
      return true
    }
    return false
  }, [])

  const sendChat = useCallback((content) => sendEvent({ type: 'chat', content }), [sendEvent])
  const sendInput = useCallback((payload) => sendEvent({ type: 'input', payload }), [sendEvent])
  const kick = useCallback((target) => sendEvent({ type: 'kick', target }), [sendEvent])
  const transfer = useCallback((target) => sendEvent({ type: 'transfer', target }), [sendEvent])
  const close = useCallback(() => sendEvent({ type: 'close' }), [sendEvent])

  return { connected, selfId, members, room, error, sendChat, sendInput, kick, transfer, close }
}
