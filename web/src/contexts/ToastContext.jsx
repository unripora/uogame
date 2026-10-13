import React, { createContext, useCallback, useContext, useState } from 'react'

// 轻提示（对齐产物的 ue.success / ue.error / ue.info 行为）
const ToastContext = createContext(null)

let seq = 0

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([])

  const remove = useCallback((id) => {
    setToasts((list) => list.filter((t) => t.id !== id))
  }, [])

  const push = useCallback(
    (type, message, duration = 2600) => {
      const id = ++seq
      setToasts((list) => [...list, { id, type, message }])
      setTimeout(() => remove(id), duration)
    },
    [remove]
  )

  const api = {
    success: (msg, d) => push('success', msg, d),
    error: (msg, d) => push('error', msg, d),
    info: (msg, d) => push('info', msg, d),
    remove,
  }

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="pointer-events-none fixed left-1/2 top-4 z-[9999] flex -translate-x-1/2 flex-col items-center gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={[
              'pointer-events-auto max-w-[90vw] rounded-full px-4 py-2 text-sm font-bold text-white shadow-lg',
              t.type === 'success'
                ? 'bg-emerald-500'
                : t.type === 'error'
                  ? 'bg-rose-500'
                  : 'bg-slate-700',
            ].join(' ')}
          >
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used within ToastProvider')
  return ctx
}
