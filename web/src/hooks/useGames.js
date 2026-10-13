import { useCallback, useEffect, useState } from 'react'
import { API } from '../api/endpoints'
import { getJSON } from '../api/client'
import { getCurrentSource, isStaticSource } from '../lib/source'
import { fetchManifest } from '../lib/sourceApi'
import { LS, lsGet, lsSet } from '../hooks/usePersistence'

// 游戏列表数据：优先读缓存（即时渲染），后台拉取最新并回写缓存
// 契约：
//   - 本地源：GET /api/games 返回**裸数组**
//   - 远程源（静态/动态）：走源清单接口（fetchManifest），大厅随当前源浏览
export function useGames() {
  const [games, setGames] = useState(() => lsGet(LS.cacheGames, []) || [])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  const reload = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const src = getCurrentSource()
      let list = []
      if (src && !src.capabilities?.local) {
        // 远程源：一次拉全量清单（静态源为 manifest，动态源为其清单端点）
        const data = await fetchManifest(src, { page: 1, pageSize: 100000 })
        list = data.items || []
      } else {
        const data = await getJSON(`${API.games}?_t=${Date.now()}`)
        list = Array.isArray(data) ? data : []
      }
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
