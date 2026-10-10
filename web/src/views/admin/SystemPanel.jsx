import React, { useEffect, useState } from 'react'
import Button from '../../components/ui/Button'
import Input from '../../components/ui/Input'
import Spinner from '../../components/ui/Spinner'
import { API } from '../../api/endpoints'
import { getJSON, putJSON, postJSON, del } from '../../api/client'
import { useToast } from '../../contexts/ToastContext'
import { useCategories } from '../../hooks/useCategories'

// 系统设置面板：开放注册 / 新用户默认分类权限 / 危险操作区
export default function SystemPanel() {
  const toast = useToast()
  const { categories } = useCategories()
  const [settings, setSettings] = useState(null)
  const [defaultPerms, setDefaultPerms] = useState([])
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    ;(async () => {
      try {
        const data = await getJSON(`${API.adminSettings}?_t=${Date.now()}`)
        // 兼容两种返回：{settings:[{key,value}]} 或扁平对象
        let flat = data || {}
        if (Array.isArray(data?.settings)) {
          flat = {}
          data.settings.forEach((s) => {
            flat[s.key] = s.value
          })
        }
        flat.show_platform_tag = flat.show_platform_tag !== 'false' && flat.show_platform_tag !== false
        // 后端公开设置与注册开关统一使用 registration_enabled 键（缺省关闭）
        flat.registration_enabled =
          flat.registration_enabled === true || flat.registration_enabled === 'true'
        // 刮削网络：代理缺省空（回退环境变量），超时缺省 60 秒
        flat.scrape_proxy = flat.scrape_proxy || ''
        flat.scrape_timeout_sec = flat.scrape_timeout_sec || '60'
        setSettings(flat)
        let perms = []
        try {
          perms = JSON.parse(flat.default_category_permissions || '[]')
        } catch {
          perms = []
        }
        setDefaultPerms(Array.isArray(perms) ? perms.map(Number) : [])
      } catch (e) {
        toast.error('获取设置失败')
        setSettings({ show_platform_tag: true })
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const togglePerm = (id) => {
    setDefaultPerms((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    )
  }

  const save = async () => {
    setSaving(true)
    try {
      // 后端 PUT /admin/settings 为单键 upsert，逐项保存
      await putJSON(API.adminSettings, {
        key: 'registration_enabled',
        value: settings.registration_enabled ? 'true' : 'false',
      })
      await putJSON(API.adminSettings, {
        key: 'default_category_permissions',
        value: JSON.stringify(defaultPerms),
      })
      await putJSON(API.adminSettings, {
        key: 'show_platform_tag',
        value: settings.show_platform_tag !== false ? 'true' : 'false',
      })
      await putJSON(API.adminSettings, {
        key: 'scrape_proxy',
        value: (settings.scrape_proxy || '').trim(),
      })
      await putJSON(API.adminSettings, {
        key: 'scrape_timeout_sec',
        value: String(settings.scrape_timeout_sec || '60').trim(),
      })
      toast.success('设置保存成功')
    } catch (e) {
      toast.error(e.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  // 平台标签显示开关：即时保存（独立于"保存设置"按钮）
  const togglePlatformTag = async (checked) => {
    setSettings((prev) => ({ ...prev, show_platform_tag: checked }))
    try {
      await putJSON(API.adminSettings, {
        key: 'show_platform_tag',
        value: checked ? 'true' : 'false',
      })
      toast.success(checked ? '已开启平台标签显示' : '已关闭平台标签显示')
    } catch (e) {
      setSettings((prev) => ({ ...prev, show_platform_tag: !checked }))
      toast.error(e.message || '保存失败')
    }
  }

  const clearChat = async () => {
    if (!confirm('警告：确定要清空聊天室的所有历史记录吗？此操作不可撤销！')) return
    try {
      await del(API.adminChat)
      toast.success('聊天记录已清空')
    } catch (e) {
      toast.error(e.message || '操作失败')
    }
  }

  const resetRank = async () => {
    if (!confirm('确认执行')) return
    try {
      await postJSON(API.adminLeaderboardsReset, {})
      toast.success('排行榜已重置')
    } catch (e) {
      toast.error(e.message || '操作失败')
    }
  }

  if (settings === null) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <section>
        <h2 className="mb-2 text-base font-black text-theme-text">全局功能开关</h2>
        <div className="rounded-xl border border-theme-border bg-theme-panel p-3">
          <label className="flex items-center justify-between">
            <div>
              <div className="text-sm font-bold text-theme-text">开放注册</div>
              <div className="text-[11px] text-theme-sub">
                允许访客创建新账号（当前：
                <span
                  className={settings.registration_enabled ? 'text-emerald-500' : 'text-rose-500'}
                >
                  {settings.registration_enabled ? '已开启' : '已关闭'}
                </span>
                ）
              </div>
            </div>
            <input
              type="checkbox"
              checked={!!settings.registration_enabled}
              onChange={(e) =>
                setSettings({ ...settings, registration_enabled: e.target.checked })
              }
            />
          </label>
          <div className="my-3 h-px bg-theme-border" />
          <label className="flex items-center justify-between">
            <div>
              <div className="text-sm font-bold text-theme-text">显示平台标签</div>
              <div className="text-[11px] text-theme-sub">
                在游戏卡片上显示按文件类型自动识别的平台标签（如 红白机 NES、任天堂 GBA）。关闭后仅隐藏显示，标签数据仍保留。
              </div>
            </div>
            <input
              type="checkbox"
              checked={settings.show_platform_tag !== false}
              onChange={(e) => togglePlatformTag(e.target.checked)}
            />
          </label>
        </div>
      </section>

      <section>
        <h2 className="mb-2 text-base font-black text-theme-text">刮削网络</h2>
        <p className="mb-2 text-[11px] text-theme-sub">
          元数据 / 封面刮削需要访问外部图库（thumbnails.libretro.com）。若所在网络需要代理，请在此填写；
          留空则回退读环境变量 HTTP_PROXY / HTTPS_PROXY（服务不读取 Windows 系统代理）。
        </p>
        <div className="rounded-xl border border-theme-border bg-theme-panel p-3">
          <label className="block">
            <div className="mb-1 text-sm font-bold text-theme-text">代理地址</div>
            <Input
              value={settings.scrape_proxy || ''}
              placeholder="http://127.0.0.1:7890（留空表示不使用代理）"
              onChange={(e) => setSettings({ ...settings, scrape_proxy: e.target.value })}
            />
          </label>
          <div className="my-3 h-px bg-theme-border" />
          <label className="block">
            <div className="mb-1 text-sm font-bold text-theme-text">单请求超时（秒）</div>
            <Input
              type="number"
              min="5"
              max="300"
              value={settings.scrape_timeout_sec || '60'}
              onChange={(e) => setSettings({ ...settings, scrape_timeout_sec: e.target.value })}
            />
            <div className="mt-1 text-[11px] text-theme-sub">
              覆盖整次请求（含图片下载），范围 5~300，默认 60。网络较慢或图库索引较大时可调大。
            </div>
          </label>
        </div>
        <Button className="mt-3" onClick={save} disabled={saving}>
          保存设置
        </Button>
      </section>

      <section>
        <h2 className="mb-2 text-base font-black text-theme-text">新用户默认分类权限</h2>
        <p className="mb-2 text-[11px] text-theme-sub">
          设置后，新注册的用户将默认拥有以下分类的访问权限
        </p>
        {categories.length === 0 ? (
          <div className="text-[13px] text-theme-sub">请先在分类配置中添加分类</div>
        ) : (
          <div className="flex flex-wrap gap-2">
            {categories.map((c) => {
              const on = defaultPerms.includes(Number(c.id))
              return (
                <button
                  key={c.id}
                  onClick={() => togglePerm(Number(c.id))}
                  className={[
                    'rounded-full px-3 py-1.5 text-xs font-bold',
                    on ? 'bg-theme-accent text-white' : 'bg-theme-panel text-theme-sub',
                  ].join(' ')}
                >
                  {c.name}
                </button>
              )
            })}
          </div>
        )}
        <Button className="mt-3" onClick={save} disabled={saving}>
          保存设置
        </Button>
      </section>

      <section>
        <h2 className="mb-2 text-base font-black text-rose-500">危险操作区</h2>
        <div className="flex flex-wrap gap-2">
          <Button variant="danger" onClick={clearChat}>
            清空聊天室
          </Button>
          <Button variant="danger" onClick={resetRank}>
            重置排行榜
          </Button>
        </div>
      </section>
    </div>
  )
}
