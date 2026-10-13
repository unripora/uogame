import React from 'react'

// 基础按钮：variant = primary | ghost | danger | subtle
const VARIANTS = {
  primary:
    'bg-theme-accent text-white hover:opacity-90 disabled:opacity-50',
  ghost:
    'bg-transparent text-theme-text border border-theme-border hover:bg-theme-panel',
  danger: 'bg-rose-500 text-white hover:bg-rose-600 disabled:opacity-50',
  subtle: 'bg-theme-panel text-theme-text hover:opacity-90 disabled:opacity-50',
}

export default function Button({
  children,
  variant = 'primary',
  className = '',
  type = 'button',
  ...rest
}) {
  return (
    <button
      type={type}
      className={[
        'inline-flex items-center justify-center gap-1 rounded-xl px-4 py-2 text-sm font-bold transition',
        VARIANTS[variant] || VARIANTS.primary,
        className,
      ].join(' ')}
      {...rest}
    >
      {children}
    </button>
  )
}
