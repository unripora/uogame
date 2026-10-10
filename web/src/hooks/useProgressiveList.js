import { useEffect, useRef, useState } from 'react'

// 渐进式渲染：一次性拿到大列表时，先只渲染前 initial 个，
// 用户滚动到哨兵元素时再追加 step 个，避免一次性挂载数千个节点（含 <img>）。
//
// 返回：{ visible, sentinelRef, done }
//   visible   - 当前应渲染的切片（list.slice(0, visible)）
//   sentinelRef - 需挂到列表末尾的占位元素 ref
//   done      - 是否已渲染完整个列表
//
// 依赖项变化（如筛选/排序变化导致 list 身份变化）时重置为 initial。
export default function useProgressiveList(list, { initial = 24, step = 24, deps = [] } = {}) {
  const total = Array.isArray(list) ? list.length : 0
  const [visible, setVisible] = useState(() => Math.min(initial, total))
  const sentinelRef = useRef(null)

  // list 变化（筛选/排序/数据更新）→ 重置
  useEffect(() => {
    setVisible(Math.min(initial, total))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [total, initial, ...deps])

  useEffect(() => {
    const el = sentinelRef.current
    if (!el) return undefined
    if (visible >= total) return undefined
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setVisible((v) => Math.min(v + step, total))
        }
      },
      { rootMargin: '600px 0px' }
    )
    io.observe(el)
    return () => io.disconnect()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, total, step])

  return { visible, sentinelRef, done: visible >= total }
}
