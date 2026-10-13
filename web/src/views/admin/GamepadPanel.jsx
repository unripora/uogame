import React, { useEffect, useRef, useState } from 'react'
import { PAD_BUTTON } from '../../hooks/useGamepad'

// 手柄调试面板：实时展示连接数 / 按键 / 摇杆 / 最近动作事件
// 用途：逐键验证手柄兼容性（不同浏览器/系统映射差异）
export default function GamepadPanel() {
  const [pads, setPads] = useState([])
  const [log, setLog] = useState([])
  const lastButtons = useRef({})

  // RAF 轮询：读取手柄快照
  useEffect(() => {
    let raf = 0
    const tick = () => {
      raf = requestAnimationFrame(tick)
      const list = navigator.getGamepads ? navigator.getGamepads() : []
      const snap = []
      for (const p of list) {
        if (!p) continue
        snap.push({
          index: p.index,
          id: p.id,
          connected: p.connected,
          mapping: p.mapping,
          buttons: p.buttons.map((b) => b.pressed),
          axes: Array.from(p.axes || []),
        })
      }
      setPads((prev) => {
        // 仅在数据变化时更新，避免无谓重渲染
        const a = JSON.stringify(prev)
        const b = JSON.stringify(snap)
        return a === b ? prev : snap
      })
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [])

  // 记录按键边沿（真实 change 日志）
  useEffect(() => {
    for (const p of pads) {
      const prev = lastButtons.current[p.index] || []
      p.buttons.forEach((down, i) => {
        if (down && !prev[i]) {
          pushLog(`#${p.index} 按下 button[${i}]`)
        }
      })
      lastButtons.current[p.index] = p.buttons
    }
  }, [pads])

  // 订阅 useGamepad 派发的业务动作
  useEffect(() => {
    const onAction = (e) => pushLog(`动作 ${e.detail?.action}`)
    window.addEventListener('gamepad:action', onAction)
    return () => window.removeEventListener('gamepad:action', onAction)
  }, [])

  const pushLog = (msg) => {
    const t = new Date().toLocaleTimeString()
    setLog((prev) => [`${t}  ${msg}`, ...prev].slice(0, 50))
  }

  // 标准映射按钮名反查表
  const btnName = (i) => {
    const hit = Object.entries(PAD_BUTTON).find(([, v]) => v === i)
    return hit ? hit[0] : `B${i}`
  }

  const connectedCount = pads.filter((p) => p.connected).length

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <div className="rounded-xl border border-theme-border bg-theme-panel px-4 py-2 text-sm font-bold text-theme-text">
          已连接手柄：{connectedCount}
        </div>
        <span className="text-xs text-theme-sub">
          请按动手柄任意按键以激活（浏览器需先有用户手势）
        </span>
      </div>

      {pads.length === 0 && (
        <div className="rounded-xl border border-theme-border p-4 text-sm text-theme-sub">
          未检测到手柄，请连接后按任意键
        </div>
      )}

      {pads.map((p) => (
        <div key={p.index} className="rounded-xl border border-theme-border bg-theme-panel p-4">
          <div className="mb-2 text-xs text-theme-sub">
            #{p.index} · {p.id} · mapping={p.mapping} ·{' '}
            {p.connected ? '已连接' : '未连接'}
          </div>

          <div className="mb-3">
            <div className="mb-1 text-xs font-bold text-theme-sub">按键</div>
            <div className="flex flex-wrap gap-1.5">
              {p.buttons.map((down, i) => (
                <span
                  key={i}
                  className={[
                    'rounded px-2 py-0.5 text-[11px] font-bold',
                    down
                      ? 'bg-theme-accent text-white'
                      : 'bg-black/10 text-theme-sub dark:bg-white/10',
                  ].join(' ')}
                >
                  {btnName(i)}
                </span>
              ))}
            </div>
          </div>

          <div>
            <div className="mb-1 text-xs font-bold text-theme-sub">摇杆 / 轴</div>
            <div className="flex flex-wrap gap-3 text-[11px] text-theme-sub">
              {p.axes.map((v, i) => (
                <span key={i}>
                  axis[{i}] = {v.toFixed(2)}
                </span>
              ))}
            </div>
          </div>
        </div>
      ))}

      <div>
        <div className="mb-1 text-xs font-bold text-theme-sub">事件日志（最近 50 条）</div>
        <div className="max-h-60 overflow-y-auto rounded-xl border border-theme-border bg-theme-panel p-3 font-mono text-[11px] text-theme-sub">
          {log.length === 0 ? (
            <div>暂无事件</div>
          ) : (
            log.map((l, i) => <div key={i}>{l}</div>)
          )}
        </div>
      </div>
    </div>
  )
}
