import { useCallback, useEffect, useState } from 'react'
import { API } from '../api/endpoints'
import { getJSON, postJSON, del } from '../api/client'

// 当前账号的游戏评分（game_id 字符串 → 分数），以及提交/取消
// 契约：GET /api/ratings/mine 返回 { "12": 4, "15": 5 }
export function useRatings() {
  const [mine, setMine] = useState({})

  const reload = useCallback(async () => {
    try {
      const data = await getJSON(API.myRatings)
      setMine(data && typeof data === 'object' ? data : {})
    } catch {
      /* 忽略：无评分不影响游玩 */
    }
  }, [])

  useEffect(() => {
    reload()
  }, [reload])

  const rate = useCallback(async (gameId, score) => {
    const res = await postJSON(API.gameRating(gameId), { score })
    setMine((m) => ({ ...m, [String(gameId)]: score }))
    return res
  }, [])

  const unrate = useCallback(async (gameId) => {
    const res = await del(API.gameRating(gameId))
    setMine((m) => {
      const next = { ...m }
      delete next[String(gameId)]
      return next
    })
    return res
  }, [])

  return { mine, rate, unrate, reload }
}