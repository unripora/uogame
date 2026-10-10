import React from 'react'

// 加载指示
export default function Spinner({ className = '' }) {
  return (
    <div
      className={[
        'inline-block h-5 w-5 animate-spin rounded-full border-2 border-theme-border border-t-theme-accent',
        className,
      ].join(' ')}
    />
  )
}
