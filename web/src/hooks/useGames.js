import { useCallback, useEffect, useState } from 'react'
import { API } from '../api/endpoints'
import { getJSON } from '../api/client'
import { LS, lsGet, lsSet } from '../hooks/usePersistence'

// 游戏列表数据：优先读缓存（即时渲染），后台拉取最新并回写缓存
// 契约：GET /api/games 返回**裸数组**
export function useGames() {
  const [games, setGames] = useState(() => lsGet(LS.cacheGames, []) || [])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  const reload = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await getJSON(`${API.games}?_t=${Date.now()}`)
      const list = Array.isArray(data) ? data : []
      setGames(list)
      lsSet(LS.cacheGames, list)
    } catch (e) {
      setError(e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    reload()
  }, [reload])

  return { games, setGames, loading, error, reload }
}
