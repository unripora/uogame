import { useEffect, useState } from 'react'
import { API } from '../api/endpoints'
import { getJSON } from '../api/client'
import { LS, lsGet, lsSet } from './usePersistence'

// 公开设置缓存键（与现有缓存键风格一致）
const CACHE_KEY = 'cache_public_settings'

// 读取公开设置（无需登录）：站点名 / 开放注册 / 是否显示平台标签
// 契约 GET /api/public/settings → { registration_enabled, site_name, show_platform_tag }
export function usePublicSettings() {
  const [settings, setSettings] = useState(
    () => lsGet(CACHE_KEY, null) || { show_platform_tag: true }
  )

  useEffect(() => {
    let alive = true
    ;(async () => {
      try {
        const data = await getJSON(`${API.publicSettings}?_t=${Date.now()}`)
        if (!alive || !data) return
        setSettings(data)
        lsSet(CACHE_KEY, data)
      } catch {
        /* 静默失败：使用缓存/默认值 */
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  return settings
}

// 是否显示平台标签（缺省 true）
export function usePlatformTagVisible() {
  const s = usePublicSettings()
  return s ? s.show_platform_tag !== false : true
}
