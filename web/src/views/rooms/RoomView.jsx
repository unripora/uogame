import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeft, Crown, LogOut, Play, Send, UserMinus, UserPlus } from 'lucide-react'
import { useAuth } from '../../contexts/AuthContext'
import { useToast } from '../../contexts/ToastContext'
import { useRoom } from '../../hooks/useRoom'
import Button from '../../components/ui/Button'
import Input from '../../components/ui/Input'
import Empty from '../../components/ui/Empty'

// 多人联机房间——房间内页面：成员列表 + 房内聊天 + 房主管理
// 事件协议见 ../hooks/useRoom.js
export default function RoomView({ roomId, password, onLeave, onPlay }) {
  const { user } = useAuth()
  const toast = useToast()
  const [messages, setMessages] = useState([])
  const [draft, setDraft] = useState('')
  const listRef = useRef(null)

  const onEvent = useCallback(
    (evt) => {
      switch (evt.type) {
        case 'chat':
          setMessages((prev) => [
            ...prev,
            { user_id: evt.user_id, nickname: evt.nickname, content: evt.content },
          ])
          break
        case 'kicked':
          toast.error('您已被移出房间')
          onLeave && onLeave()
          break
        case 'room_closed':
          toast.info('房间已关闭')
          onLeave && onLeave()
          break
        case 'owner_transferred':
          break
        default:
          break
      }
    },
    [toast, onLeave]
  )

  const { connected, selfId, members, room, error, sendChat, kick, transfer, close } = useRoom(
    roomId,
    password,
    onEvent
  )

  useEffect(() => {
    if (error) toast.error(error)
  }, [error, toast])

  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = listRef.current.scrollHeight
  }, [messages])

  const isOwner = members.some((m) => m.user_id === selfId && m.is_owner)

  const submit = (e) => {
    e.preventDefault()
    const text = draft.trim()
    if (!text) return
    if (sendChat(text)) setDraft('')
  }

  const doClose = () => {
    close()
  }

  return (
    <div className="flex h-full flex-col p-4">
      {/* 头部 */}
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Button variant="ghost" onClick={onLeave}>
            <ArrowLeft size={16} /> 退出
          </Button>
          <div className="text-sm font-black text-theme-text">
            {room?.name || `房间 ${roomId}`}
            <span className="ml-2 text-[11px] font-normal text-theme-sub">
              {connected ? '已连接' : '连接中...'}
            </span>
          </div>
        </div>
        {isOwner ? (
          <div className="flex gap-2">
            {onPlay ? (
              <Button variant="ghost" onClick={() => onPlay(room)}>
                <Play size={14} /> 开始游戏
              </Button>
            ) : null}
            <Button variant="danger" onClick={doClose}>
              <LogOut size={14} /> 关闭房间
            </Button>
          </div>
        ) : null}
      </div>

      <div className="grid min-h-0 flex-1 gap-3 md:grid-cols-[220px_1fr]">
        {/* 成员列表 */}
        <section className="rounded-2xl border border-theme-border bg-theme-panel p-3">
          <h3 className="mb-2 text-xs font-black text-theme-sub">
            成员 ({members.length}/{room?.max_users || '-'})
          </h3>
          <div className="space-y-1">
            {members.map((m) => (
              <MemberRow
                key={m.user_id}
                member={m}
                isSelf={m.user_id === selfId}
                canManage={isOwner && m.user_id !== selfId}
                onKick={() => kick(m.user_id)}
                onTransfer={() => transfer(m.user_id)}
              />
            ))}
          </div>
        </section>

        {/* 聊天区 */}
        <section className="flex min-h-0 flex-col rounded-2xl border border-theme-border bg-theme-panel p-3">
          <div ref={listRef} className="flex-1 space-y-2 overflow-y-auto">
            {messages.length === 0 ? (
              <Empty text="房内畅聊，商量一下怎么配合吧..." />
            ) : (
              messages.map((m, i) => (
                <div key={i} className="text-sm">
                  <span className="mr-2 text-[11px] text-theme-sub">
                    {m.nickname || '玩家'}
                  </span>
                  <span className="break-words text-theme-text">{m.content}</span>
                </div>
              ))
            )}
          </div>
          <form onSubmit={submit} className="mt-2 flex gap-2">
            <Input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={user ? '说点什么...' : '登录后可聊天'}
              disabled={!user}
            />
            <Button type="submit" disabled={!user}>
              <Send size={16} />
            </Button>
          </form>
        </section>
      </div>
    </div>
  )
}

// 成员行
function MemberRow({ member, isSelf, canManage, onKick, onTransfer }) {
  return (
    <div className="flex items-center justify-between rounded-xl px-2 py-1.5 hover:bg-theme-bg">
      <div className="flex min-w-0 items-center gap-1.5 text-sm text-theme-text">
        {member.is_owner ? <Crown size={13} className="text-amber-500" /> : null}
        <span className="truncate">{member.nickname || member.username || '玩家'}</span>
        {isSelf ? <span className="text-[10px] text-theme-sub">(我)</span> : null}
      </div>
      {canManage ? (
        <div className="flex gap-1">
          <button
            onClick={onTransfer}
            title="转让房主"
            className="rounded p-1 text-theme-sub hover:text-theme-accent"
          >
            <UserPlus size={14} />
          </button>
          <button
            onClick={onKick}
            title="移出房间"
            className="rounded p-1 text-theme-sub hover:text-rose-500"
          >
            <UserMinus size={14} />
          </button>
        </div>
      ) : null}
    </div>
  )
}
