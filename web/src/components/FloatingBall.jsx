import React, { useEffect, useRef, useState } from 'react'
import { Gamepad2, X } from 'lucide-react'

// 可拖动悬浮球：点击展开功能菜单（游戏内快捷操作）
// items: [{ key, label, icon, onClick }]
// children: 可选，渲染在菜单底部（用于常驻控件，如倍速选择，点击不收起菜单）
// 位置记忆在 localStorage，拖动不会误触发菜单
const POS_KEY = 'uogame_ball_pos'
const BALL = 48
const MENU_W = 184

function clamp(v, min, max) {
  return Math.max(min, Math.min(max, v))
}

function defaultPos() {
  return { x: Math.max(8, window.innerWidth - BALL - 16), y: Math.max(8, window.innerHeight - 160) }
}

export default function FloatingBall({ items = [], children = null }) {
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState(() => {
    try {
      const raw = JSON.parse(localStorage.getItem(POS_KEY) || 'null')
      if (raw && typeof raw.x === 'number' && typeof raw.y === 'number') return raw
    } catch {
      /* ignore */
    }
    return defaultPos()
  })
  const posRef = useRef(pos)
  useEffect(() => {
    posRef.current = pos
  }, [pos])

  const drag = useRef({ active: false, moved: false, sx: 0, sy: 0, ox: 0, oy: 0 })

  const onPointerDown = (e) => {
    drag.current = {
      active: true,
      moved: false,
      sx: e.clientX,
      sy: e.clientY,
      ox: posRef.current.x,
      oy: posRef.current.y,
    }
    try {
      e.currentTarget.setPointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
  }

  const onPointerMove = (e) => {
    const d = drag.current
    if (!d.active) return
    const dx = e.clientX - d.sx
    const dy = e.clientY - d.sy
    if (Math.abs(dx) > 4 || Math.abs(dy) > 4) d.moved = true
    if (!d.moved) return
    setPos({
      x: clamp(d.ox + dx, 8, window.innerWidth - BALL - 8),
      y: clamp(d.oy + dy, 8, window.innerHeight - BALL - 8),
    })
  }

  const onPointerUp = () => {
    const d = drag.current
    if (!d.active) return
    d.active = false
    if (d.moved) {
      try {
        localStorage.setItem(POS_KEY, JSON.stringify(posRef.current))
      } catch {
        /* ignore */
      }
    }
  }

  // 展开/收起走 click：兼容键盘与合成点击；拖动结束产生的那次 click 需忽略
  const onClick = () => {
    const d = drag.current
    if (d.moved) {
      d.moved = false
      return
    }
    setOpen((o) => !o)
  }

  const menuLeft = clamp(pos.x + BALL - MENU_W, 8, Math.max(8, window.innerWidth - MENU_W - 8))
  const menuBottom = Math.max(8, window.innerHeight - pos.y + 12)

  return (
    <>
      {open && (
        <div className="fixed z-40 flex flex-col gap-1.5" style={{ left: menuLeft, bottom: menuBottom, width: MENU_W }}>
          {items.map((it) => (
            <button
              key={it.key}
              onClick={() => {
                setOpen(false)
                if (it.onClick) it.onClick()
              }}
              className="flex items-center gap-2 rounded-xl border border-white/15 bg-black/75 px-3 py-2 text-left text-xs font-bold text-white backdrop-blur transition hover:bg-black/90"
            >
              {it.icon}
              {it.label}
            </button>
          ))}
          {children}
        </div>
      )}

      <button
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onClick={onClick}
        style={{ left: pos.x, top: pos.y, touchAction: 'none' }}
        className="fixed z-40 flex h-12 w-12 items-center justify-center rounded-full border border-white/20 bg-black/60 text-white shadow-lg backdrop-blur transition active:scale-95"
        title="游戏助手"
      >
        {open ? <X size={20} /> : <Gamepad2 size={20} />}
      </button>
    </>
  )
}