import React from 'react'

// 下拉选择
export default function Select({ children, className = '', ...rest }) {
  return (
    <select
      className={[
        'w-full rounded-xl border border-theme-border bg-theme-panel px-3 py-2 text-sm text-theme-text',
        'focus:border-theme-accent focus:outline-none',
        className,
      ].join(' ')}
      {...rest}
    >
      {children}
    </select>
  )
}
