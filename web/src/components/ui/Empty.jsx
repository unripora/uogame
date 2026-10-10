import React from 'react'

// 空状态占位
export default function Empty({ icon = null, text = '暂无数据', className = '' }) {
  return (
    <div
      className={[
        'flex flex-col items-center justify-center gap-2 py-12 text-theme-sub',
        className,
      ].join(' ')}
    >
      {icon}
      <span className="text-sm">{text}</span>
    </div>
  )
}
