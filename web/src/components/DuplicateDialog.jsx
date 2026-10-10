import React, { useEffect, useState } from 'react'
import Modal from './ui/Modal'
import Button from './ui/Button'

// 导入重复确认弹窗
// 列表逐项展示「待导入 × 库中已存在」，支持多选：
//   勾选   → 覆盖（用新包更新库中既有记录）
//   未勾选 → 跳过（不导入）
// 判据徽标：fingerprint（内容指纹）| name（名称）
export default function DuplicateDialog({ open, items = [], busy = false, onConfirm, onSkipAll }) {
  // 勾选项集合（默认全部不勾 = 全部跳过，安全默认）
  const [checked, setChecked] = useState(() => new Set())

  // 每次打开时重置为全不选
  useEffect(() => {
    if (open) setChecked(new Set())
  }, [open])

  const toggle = (idx) => {
    setChecked((prev) => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
  }

  const checkAll = () => setChecked(new Set(items.map((it) => it.idx)))
  const clearAll = () => setChecked(new Set())

  const reasonLabel = (r) => (r === 'fingerprint' ? '内容指纹' : r === 'name' ? '名称' : '未知')

  return (
    <Modal
      open={open}
      title={`发现 ${items.length} 个重复游戏`}
      onClose={busy ? undefined : onSkipAll}
      footer={
        <>
          <Button variant="ghost" onClick={onSkipAll} disabled={busy}>
            全部跳过
          </Button>
          <Button onClick={() => onConfirm(Array.from(checked))} disabled={busy}>
            确认覆盖（已选 {checked.size} 项）
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <p className="text-xs text-theme-sub">
          勾选后将<strong className="text-theme-text">覆盖</strong>库中已有游戏；未勾选则
          <strong className="text-theme-text">跳过</strong>导入。默认全部跳过，请按需勾选。
        </p>

        {/* 表头 */}
        <div className="flex items-center gap-2 border-b border-theme-border pb-1 text-[11px] font-bold text-theme-sub">
          <span className="w-5" />
          <span className="flex-1">待导入</span>
          <span className="w-16 shrink-0 text-center">判据</span>
          <span className="flex-1">库中已有</span>
        </div>

        {/* 列表 */}
        <div className="max-h-64 space-y-1 overflow-y-auto">
          {items.map((it) => (
            <label
              key={it.idx}
              className="flex cursor-pointer items-center gap-2 rounded-lg px-1 py-1.5 hover:bg-theme-bg"
            >
              <input
                type="checkbox"
                className="h-4 w-4 shrink-0 accent-theme-accent"
                checked={checked.has(it.idx)}
                onChange={() => toggle(it.idx)}
              />
              <span className="min-w-0 flex-1 truncate text-sm font-bold text-theme-text">
                {it.name}
              </span>
              <span className="w-16 shrink-0 text-center">
                <span className="inline-block rounded px-1.5 py-0.5 text-[10px] font-bold bg-theme-panel text-theme-sub">
                  {reasonLabel(it.reason)}
                </span>
              </span>
              <span className="min-w-0 flex-1 truncate text-sm text-theme-sub">
                #{it.existingId} {it.existingName}
              </span>
            </label>
          ))}
        </div>

        {/* 批量操作 */}
        <div className="flex items-center gap-2 border-t border-theme-border pt-2 text-xs">
          <Button variant="subtle" className="px-2 py-1" onClick={checkAll} disabled={busy}>
            全选
          </Button>
          <Button variant="subtle" className="px-2 py-1" onClick={clearAll} disabled={busy}>
            全不选
          </Button>
          <span className="ml-auto text-theme-sub">
            共 {items.length} 项，已选 {checked.size} 项
          </span>
        </div>
      </div>
    </Modal>
  )
}
