import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Users, Send, Trophy } from 'lucide-react'
import { API } from '../api/endpoints'
import { getJSON } from '../api/client'
import { useWebSocket } from '../hooks/useWebSocket'
import { useAuth } from '../contexts/AuthContext'
import Empty from '../components/ui/Empty'
import Spinner from '../components/ui/Spinner'
import { formatPlayTime } from '../lib/format'

// 社区：聊天室（WebSocket）+ 排行榜（时长 / 热门）
export default function CommunityView({ onOpenRooms }) {
  const { user } = useAuth()
  const [tab, setTab] = useState('chat') // chat | rank
  const [messages, setMessages] = useState([])
  const [draft, setDraft] = useState('')
  const listRef = useRef(null)

  const { connected, send } = useWebSocket((msg) => {
    setMessages((prev) => [...prev, msg])
  })

  // 加载历史消息（契约：裸数组）
  useEffect(() => {
    if (tab !== 'chat') return
    ;(async () => {
      try {
        const data = await getJSON(`${API.chatHistory}?t=${Date.now()}`)
        setMessages(Array.isArray(data) ? data : [])
      } catch {
        setMessages([])
      }
    })()
  }, [tab])

  useEffect(() => {
    if (listRef.current) {
      listRef.current.scrollTop = listRef.current.scrollHeight
    }
  }, [messages, tab])

  const submit = (e) => {
    e.preventDefault()
    const text = draft.trim()
    if (!text) return
    if (send(text)) setDraft('')
  }

  return (
    <div className="flex h-full flex-col p-4">
      <div className="mb-3 flex gap-2">
        <button
          onClick={() => setTab('chat')}
          className={[
            'rounded-full px-4 py-1.5 text-sm font-bold',
            tab === 'chat' ? 'bg-theme-accent text-white' : 'bg-theme-panel text-theme-sub',
          ].join(' ')}
        >
          聊天
        </button>
        <button
          onClick={() => setTab('rank')}
          className={[
            'rounded-full px-4 py-1.5 text-sm font-bold',
            tab === 'rank' ? 'bg-theme-accent text-white' : 'bg-theme-panel text-theme-sub',
          ].join(' ')}
        >
          排行
        </button>
        <button
          onClick={() => onOpenRooms && onOpenRooms()}
          className="ml-auto flex items-center gap-1 rounded-full bg-theme-panel px-4 py-1.5 text-sm font-bold text-theme-sub hover:text-theme-accent"
        >
          <Users size={15} /> 联机房间
        </button>
      </div>

      {tab === 'chat' ? (
        <>
          <div className="mb-1 text-[11px] text-theme-sub">
            {connected ? '活跃中' : '连接中...'}
          </div>
          <div
            ref={listRef}
            className="flex-1 space-y-3 overflow-y-auto rounded-2xl border border-theme-border bg-theme-panel p-3"
          >
            {messages.length === 0 ? (
              <Empty text="快来开启第一个话题吧..." />
            ) : (
              messages.map((m, i) => (
                <div key={m.id || i} className="flex gap-2">
                  <div className="h-8 w-8 shrink-0 overflow-hidden rounded-full bg-theme-border">
                    {m.avatar ? (
                      <img src={m.avatar} alt="" className="h-full w-full object-cover" />
                    ) : null}
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-1 text-[11px] text-theme-sub">
                      <span>{m.nickname || m.username || '匿名'}</span>
                      {m.is_manager ? (
                        <span className="inline-flex items-center rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[9px] font-bold text-amber-500">
                          管理员
                        </span>
                      ) : null}
                    </div>
                    <div className="break-words text-sm text-theme-text">{m.content}</div>
                  </div>
                </div>
              ))
            )}
          </div>
          <form onSubmit={submit} className="mt-2 flex gap-2">
            <input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={user ? '聊点什么吧...' : '登录后参与聊天'}
              disabled={!user}
              className="flex-1 rounded-xl border border-theme-border bg-theme-panel px-3 py-2 text-sm text-theme-text placeholder:text-theme-sub focus:border-theme-accent focus:outline-none"
            />
            <button
              type="submit"
              disabled={!user}
              className="rounded-xl bg-theme-accent px-4 text-white disabled:opacity-50"
            >
              <Send size={16} />
            </button>
          </form>
        </>
      ) : (
        <RankPanel />
      )}
    </div>
  )
}

// 排行榜面板
function RankPanel() {
  const [playtime, setPlaytime] = useState(null)
  const [hot, setHot] = useState(null)

  useEffect(() => {
    ;(async () => {
      try {
        const [pt, hg] = await Promise.all([
          getJSON(API.leaderboardPlaytime),
          getJSON(API.leaderboardHotGames),
        ])
        setPlaytime(Array.isArray(pt) ? pt : [])
        setHot(Array.isArray(hg) ? hg : [])
      } catch {
        setPlaytime([])
        setHot([])
      }
    })()
  }, [])

  const loading = playtime === null || hot === null
  if (loading) {
    return (
      <div className="flex flex-1 justify-center py-12">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="flex-1 space-y-4 overflow-y-auto">
      <section className="rounded-2xl border border-theme-border bg-theme-panel p-3">
        <h3 className="mb-2 flex items-center gap-1 text-sm font-black text-theme-text">
          <Trophy size={16} /> 总时长
        </h3>
        {playtime.length === 0 ? (
          <Empty text="暂无榜单数据" />
        ) : (
          playtime.map((r, i) => (
            <div
              key={i}
              className="flex items-center justify-between border-b border-theme-border py-2 text-sm last:border-0"
            >
              <span className="text-theme-text">
                {i + 1}. {r.nickname || '玩家'}
              </span>
              <span className="text-theme-sub">{formatPlayTime(r.playTime)}</span>
            </div>
          ))
        )}
      </section>

      <section className="rounded-2xl border border-theme-border bg-theme-panel p-3">
        <h3 className="mb-2 text-sm font-black text-theme-text">热度</h3>
        {hot.length === 0 ? (
          <Empty text="暂无榜单数据" />
        ) : (
          hot.map((r, i) => (
            <div
              key={i}
              className="flex items-center justify-between border-b border-theme-border py-2 text-sm last:border-0"
            >
              <span className="text-theme-text">
                {i + 1}. {r.name}
              </span>
              <span className="text-theme-sub">游玩人次: {r.players || 0}</span>
            </div>
          ))
        )}
      </section>
    </div>
  )
}
