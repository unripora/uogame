import { useCallback, useEffect, useState } from 'react'
import { API } from '../api/endpoints'
import { getJSON } from '../api/client'

// 全部标签（去重）：GET /api/tags 返回裸数组
// 优先从游戏列表聚合，保证离线/无接口时也能用；后台再拉取一次覆盖
export function useTags(fallbackGames = []) {
  const [tags, setTags] = useState(() => collectTags(fallbackGames))
  const [loading, setLoading] = useState(false)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getJSON(API.tags)
      if (Array.isArray(data)) setTags(data)
    } catch {
      /* 忽略：回退到本地聚合 */
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    // 本地先聚合一份，接口返回后覆盖
    const local = collectTags(fallbackGames)
    if (local.length) setTags((prev) => (prev.length ? prev : local))
    reload()
  }, [reload, fallbackGames])

  return { tags, loading, reload }
}

// 从游戏列表聚合标签（去重）
function collectTags(games) {
  if (!Array.isArray(games)) return []
  const set = new Set()
  games.forEach((g) => {
    if (Array.isArray(g.tags)) {
      g.tags.forEach((t) => t && set.add(t))
    }
  })
  return Array.from(set)
}
