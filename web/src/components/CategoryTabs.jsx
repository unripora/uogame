import React from 'react'

// 分类标签导航：全部 / 默认分类 / 各分类
export default function CategoryTabs({ categories, active, onChange }) {
  const tabs = [{ id: 0, name: '全部' }, ...(categories || [])]
  return (
    <div className="flex flex-wrap gap-2">
      {tabs.map((c) => {
        const isActive = String(active) === String(c.id)
        return (
          <button
            key={c.id}
            onClick={() => onChange && onChange(c.id)}
            className={[
              'rounded-full px-4 py-1.5 text-sm font-bold transition',
              isActive
                ? 'bg-theme-accent text-white'
                : 'bg-theme-panel text-theme-sub hover:text-theme-text',
            ].join(' ')}
          >
            {c.name}
          </button>
        )
      })}
    </div>
  )
}
