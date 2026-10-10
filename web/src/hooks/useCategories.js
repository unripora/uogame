import { useCallback, useEffect, useState } from 'react'
import { API } from '../api/endpoints'
import { getJSON } from '../api/client'
import { LS, lsGet, lsSet } from '../hooks/usePersistence'

// 分类列表数据：契约 GET /api/categories 返回**裸数组**
export function useCategories() {
  const [categories, setCategories] = useState(
    () => lsGet(LS.cacheCategories, []) || []
  )
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  const reload = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await getJSON(`${API.categories}?_t=${Date.now()}`)
      const list = Array.isArray(data) ? data : []
      setCategories(list)
      lsSet(LS.cacheCategories, list)
    } catch (e) {
      setError(e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    reload()
  }, [reload])

  return { categories, setCategories, loading, error, reload }
}
