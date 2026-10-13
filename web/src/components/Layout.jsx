import React, { useEffect, useRef } from 'react'
import { Gamepad2, MessageCircle, User, Settings } from 'lucide-react'
import { useAuth } from '../contexts/AuthContext'

// 应用外壳：顶部标题栏 + 底部导航（移动端友好）
// 视图：games（游戏）/ community（社区）/ profile（我的）/ admin（管理面板）
const NAV = [
  { id: 'games', label: '游戏', icon: Gamepad2 },
  { id: 'community', label: '社区', icon: MessageCircle },
  { id: 'profile', label: '我的', icon: User },
]

// 滚动位置缓存（模块级）：按视图记忆，跨组件卸载/重挂载保留
// 进入游戏时 Layout 会整体卸载，返回后据此恢复大厅滚动位置，避免回到顶部
const scrollCache = {}

export default function Layout({ view, onNavigate, children }) {
  const { isAdmin } = useAuth()
  const items = isAdmin
    ? [...NAV, { id: 'admin', label: '管理', icon: Settings }]
    : NAV

  const scrollRef = useRef(null)

  // 切换视图时记录旧视图位置；挂载/视图变化后恢复当前视图位置
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const target = scrollCache[view] || 0
    // 恢复标志：用户主动滚动（滚轮/触摸）后立即停止自动恢复，避免与用户操作打架
    let done = false
    const userScroll = () => {
      done = true
    }
    const restore = () => {
      // 内容（图片/网格）异步渲染，初始高度可能不足导致被截断，
      // 故在若干帧内反复校正，直到接近目标或高度足够
      if (done) return
      const max = el.scrollHeight - el.clientHeight
      if (max <= 0) return // 暂无滚动空间，等后续帧
      el.scrollTop = Math.min(target, max)
      if (Math.abs(el.scrollTop - target) <= 1) done = true
    }

    const onScroll = () => {
      // 卸载瞬间容器高度塌缩会触发一次 scrollTop=0 的 scroll 事件，
      // 此时无滚动空间，忽略写入，避免覆盖上一次的有效位置
      if (el.scrollHeight - el.clientHeight <= 0) return
      scrollCache[view] = el.scrollTop
    }

    el.addEventListener('scroll', onScroll, { passive: true })
    // 用户交互（滚轮/触摸/键盘）即视为接管，停止自动恢复
    el.addEventListener('wheel', userScroll, { passive: true })
    el.addEventListener('touchstart', userScroll, { passive: true })
    el.addEventListener('keydown', userScroll)

    // 首帧立即尝试，并在接下来的 ~30 帧内持续校正（覆盖列表/图片逐渐撑高的过程）
    el.scrollTop = target
    let raf
    let frames = 0
    const tick = () => {
      restore()
      if (!done && frames < 30) {
        frames += 1
        raf = requestAnimationFrame(tick)
      }
    }
    if (target > 0) raf = requestAnimationFrame(tick)

    return () => {
      if (raf) cancelAnimationFrame(raf)
      // 仅在容器仍有滚动空间时才记录，防止卸载塌缩把有效位置覆盖为 0
      if (el.scrollHeight - el.clientHeight > 0) scrollCache[view] = el.scrollTop
      el.removeEventListener('scroll', onScroll)
      el.removeEventListener('wheel', userScroll)
      el.removeEventListener('touchstart', userScroll)
      el.removeEventListener('keydown', userScroll)
    }
  }, [view])

  return (
    <div className="flex h-full flex-col">
      <div ref={scrollRef} className="flex-1 overflow-y-auto pb-16">{children}</div>
      <nav className="fixed bottom-0 left-0 right-0 z-40 flex border-t border-theme-border bg-theme-panel">
        {items.map((it) => {
          const Icon = it.icon
          const active = view === it.id
          return (
            <button
              key={it.id}
              onClick={() => onNavigate && onNavigate(it.id)}
              className={[
                'flex flex-1 flex-col items-center gap-0.5 py-2 text-[11px] font-bold transition',
                active ? 'text-theme-accent' : 'text-theme-sub',
              ].join(' ')}
            >
              <Icon size={20} />
              {it.label}
            </button>
          )
        })}
      </nav>
    </div>
  )
}
