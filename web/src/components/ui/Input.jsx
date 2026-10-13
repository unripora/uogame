import React from 'react'

// 文本输入框
export default function Input({ className = '', ...rest }) {
  return (
    <input
      className={[
        'w-full rounded-xl border border-theme-border bg-theme-panel px-3 py-2 text-sm text-theme-text',
        'placeholder:text-theme-sub focus:border-theme-accent focus:outline-none',
        className,
      ].join(' ')}
      {...rest}
    />
  )
}
