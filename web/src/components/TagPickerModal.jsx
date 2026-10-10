import React, { useEffect, useMemo, useState } from 'react'
import { Search, Check } from 'lucide-react'
import Modal from './ui/Modal'
import Button from './ui/Button'

// 通用标签多选弹窗：顶部搜索 + 标签列表多选（已选置顶）
// 用于白名单「标签规则」与首页「标签筛选」，避免平铺展示全部标签
export default function TagPickerModal({
  open,
  title = '选择标签',
  allTags = [],
  selected = [],
  onClose,
  onConfirm,
}) {
  const [keyword, setKeyword] = useState('')
  const [picked, setPicked] = useState([])

  // 每次打开时同步初始选择并清空搜索
  useEffect(() => {
    if (open) {
      setKeyword('')
      setPicked(Array.isArray(selected) ? [...selected] : [])
    }
  }, [open, selected])

  const list = useMemo(() => {
    const uniq = Array.from(new Set((allTags || []).filter(Boolean)))
    // 已选置顶，便于确认
    const sorted = uniq.sort((a, b) => (picked.includes(b) ? 1 : 0) - (picked.includes(a) ? 1 : 0))
    const kw = keyword.trim().toLowerCase()
    return kw ? sorted.filter((t) => t.toLowerCase().includes(kw)) : sorted
  }, [allTags, keyword, picked])

  const toggle = (tag) => {
    setPicked((prev) => (prev.includes(tag) ? prev.filter((t) => t !== tag) : [...prev, tag]))
  }

  return (
    <Modal
      open={open}
      title={title}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button onClick={() => onConfirm && onConfirm(picked)}>确定（{picked.length}）</Button>
        </>
      }
    >
      <div className="relative mb-3">
        <Search
          size={16}
          className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-theme-sub"
        />
        <input
          autoFocus
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          placeholder="搜索标签..."
          className="w-full rounded-xl border border-theme-border bg-theme-panel py-2 pl-9 pr-3 text-sm text-theme-text placeholder:text-theme-sub focus:border-theme-accent focus:outline-none"
        />
      </div>

      <div className="flex max-h-64 flex-wrap gap-2 overflow-y-auto">
        {list.length === 0 ? (
          <div className="w-full py-6 text-center text-xs text-theme-sub">暂无匹配标签</div>
        ) : (
          list.map((t) => {
            const on = picked.includes(t)
            return (
              <button
                key={t}
                type="button"
                onClick={() => toggle(t)}
                className={[
                  'flex items-center gap-1 rounded-full border px-3 py-1 text-[11px] font-bold transition',
                  on
                    ? 'border-theme-accent bg-theme-accent text-white'
                    : 'border-theme-border bg-theme-panel text-theme-sub hover:text-theme-accent',
                ].join(' ')}
              >
                {on && <Check size={12} />}
                {t}
              </button>
            )
          })
        )}
      </div>
    </Modal>
  )
}