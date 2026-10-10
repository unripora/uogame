import React from 'react'

// 模态框：点击遮罩或关闭按钮关闭
export default function Modal({ open, title, children, onClose, footer }) {
  if (!open) return null
  return (
    <div
      className="fixed inset-0 z-[9000] flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg rounded-2xl border border-theme-border bg-theme-panel p-5 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {title && (
          <div className="mb-4 flex items-center justify-between">
            <h3 className="text-base font-black text-theme-text">{title}</h3>
            <button
              className="text-theme-sub hover:text-theme-text"
              onClick={onClose}
              aria-label="关闭"
            >
              ✕
            </button>
          </div>
        )}
        <div className="text-sm text-theme-text">{children}</div>
        {footer && <div className="mt-5 flex justify-end gap-2">{footer}</div>}
      </div>
    </div>
  )
}
