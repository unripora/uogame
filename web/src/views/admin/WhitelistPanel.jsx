import React, { useEffect, useState } from 'react'
import { Plus, Save, Trash2 } from 'lucide-react'
import Button from '../../components/ui/Button'
import Input from '../../components/ui/Input'
import Select from '../../components/ui/Select'
import Spinner from '../../components/ui/Spinner'
import TagPickerModal from '../../components/TagPickerModal'
import { API } from '../../api/endpoints'
import { getJSON, putJSON } from '../../api/client'
import { useToast } from '../../contexts/ToastContext'
import { useCategories } from '../../hooks/useCategories'
import { useTags } from '../../hooks/useTags'

// 白名单分类配置面板
// 每个分类一行：分类（下拉选现有分类 / 新建）+ 标签规则（精确命中）+ 关键词（包含匹配）
// 保存到 settings.whitelist_categories
const NEW_OPT = '__new__'

export default function WhitelistPanel() {
  const toast = useToast()
  const { categories } = useCategories()
  const { tags: allTags } = useTags([])
  const [groups, setGroups] = useState(null)
  const [saving, setSaving] = useState(false)
  const [tagPickerIdx, setTagPickerIdx] = useState(-1) // 正在选标签的白名单组下标

  useEffect(() => {
    ;(async () => {
      try {
        const data = await getJSON(`${API.adminWhitelist}?_t=${Date.now()}`)
        const list = Array.isArray(data && data.categories) ? data.categories : []
        setGroups(
          list.map((g) => ({
            category_id: Number(g.category_id) || 0,
            category_name: g.category_name || '',
            keywordsText: (g.keywords || []).join('、'),
            tags: Array.isArray(g.tags) ? g.tags : [],
            isNew: !(Number(g.category_id) > 0),
          }))
        )
      } catch (e) {
        toast.error(e.message || '获取白名单失败')
        setGroups([])
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const patchGroup = (idx, patch) => {
    setGroups((prev) => prev.map((g, i) => (i === idx ? { ...g, ...patch } : g)))
  }

  // 分类下拉：选择现有分类，或切到「新建分类」
  const onPickCategory = (idx, value) => {
    if (value === NEW_OPT) {
      patchGroup(idx, { isNew: true, category_id: 0 })
      return
    }
    const cat = (categories || []).find((c) => String(c.id) === String(value))
    if (cat) patchGroup(idx, { isNew: false, category_id: cat.id, category_name: cat.name })
  }

  const toggleTag = (idx, tag) => {
    setGroups((prev) =>
      prev.map((g, i) => {
        if (i !== idx) return g
        const has = g.tags.includes(tag)
        return { ...g, tags: has ? g.tags.filter((t) => t !== tag) : [...g.tags, tag] }
      })
    )
  }

  const addGroup = () => {
    setGroups((prev) => [
      ...prev,
      { category_id: 0, category_name: '', keywordsText: '', tags: [], isNew: true },
    ])
  }

  const removeGroup = (idx) => {
    setGroups((prev) => prev.filter((_, i) => i !== idx))
  }

  const save = async () => {
    setSaving(true)
    try {
      const payload = groups.map((g) => ({
        category_id: Number(g.category_id) || 0,
        category_name: String(g.category_name || '').trim(),
        keywords: String(g.keywordsText || '')
          .split(/[、,，\n\r]+/)
          .map((s) => s.trim())
          .filter(Boolean),
        tags: g.tags || [],
      }))
      if (payload.some((p) => !p.category_name)) {
        toast.error('存在未命名的分类，请填写分类名称')
        return
      }
      await putJSON(API.adminWhitelist, { categories: payload })
      toast.success('白名单已保存')
    } catch (e) {
      toast.error(e.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  if (groups === null) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-black text-theme-text">白名单分类配置</h2>
        <Button variant="ghost" onClick={addGroup}>
          <Plus size={14} /> 新增词库
        </Button>
      </div>
      <p className="text-[11px] text-theme-sub">
        <b>标签</b>为精确匹配：游戏带该标签即归入此分类（优先于关键词）；<b>关键词</b>
        用「、」或逗号分隔，对「游戏名 + 标签」做包含匹配。导入与「白名单识别」时按此规则自动归类。
      </p>

      <div className="space-y-3">
        {groups.map((g, idx) => {
          return (
            <div
              key={idx}
              className="rounded-xl border border-theme-border bg-theme-panel p-3"
            >
              <div className="mb-2 flex items-center gap-2">
                <Select
                  className="max-w-[220px]"
                  value={g.isNew ? NEW_OPT : String(g.category_id)}
                  onChange={(e) => onPickCategory(idx, e.target.value)}
                >
                  {categories.map((c) => (
                    <option key={c.id} value={String(c.id)}>
                      {c.name}
                    </option>
                  ))}
                  <option value={NEW_OPT}>＋ 新建分类…</option>
                </Select>
                {g.isNew ? (
                  <Input
                    className="max-w-[200px]"
                    placeholder="新分类名称（如 休闲解压）"
                    value={g.category_name}
                    onChange={(e) => patchGroup(idx, { category_name: e.target.value })}
                  />
                ) : (
                  <span className="text-[11px] text-theme-sub">分类ID: {g.category_id}</span>
                )}
                <button
                  className="ml-auto text-theme-sub hover:text-rose-500"
                  onClick={() => removeGroup(idx)}
                  title="删除"
                >
                  <Trash2 size={16} />
                </button>
              </div>

              <div className="mb-2">
                <div className="mb-1 text-[11px] font-bold text-theme-sub">
                  标签规则（精确命中）
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {g.tags.length === 0 && (
                    <span className="text-[11px] text-theme-sub">未设置标签</span>
                  )}
                  {g.tags.map((t) => (
                    <span
                      key={t}
                      className="flex items-center gap-1 rounded-full bg-theme-accent px-3 py-1 text-[11px] font-bold text-white"
                    >
                      {t}
                      <button
                        type="button"
                        onClick={() => toggleTag(idx, t)}
                        className="text-white/80 hover:text-white"
                        title="移除"
                      >
                        ✕
                      </button>
                    </span>
                  ))}
                  <button
                    type="button"
                    onClick={() => setTagPickerIdx(idx)}
                    className="flex items-center gap-1 rounded-full border border-dashed border-theme-border px-3 py-1 text-[11px] font-bold text-theme-sub hover:text-theme-accent"
                  >
                    <Plus size={12} /> 添加标签
                  </button>
                </div>
              </div>

              <textarea
                className="h-20 w-full resize-y rounded-xl border border-theme-border bg-theme-panel px-3 py-2 text-sm text-theme-text placeholder:text-theme-sub focus:border-theme-accent focus:outline-none"
                placeholder="关键词列表..."
                value={g.keywordsText}
                onChange={(e) => patchGroup(idx, { keywordsText: e.target.value })}
              />
            </div>
          )
        })}
      </div>

      <div className="flex justify-end">
        <Button onClick={save} disabled={saving}>
          <Save size={14} /> 保存白名单
        </Button>
      </div>

      <TagPickerModal
        open={tagPickerIdx >= 0 && !!groups[tagPickerIdx]}
        title="选择标签规则"
        allTags={allTags}
        selected={tagPickerIdx >= 0 && groups[tagPickerIdx] ? groups[tagPickerIdx].tags : []}
        onClose={() => setTagPickerIdx(-1)}
        onConfirm={(picked) => {
          const idx = tagPickerIdx
          setTagPickerIdx(-1)
          if (idx >= 0) patchGroup(idx, { tags: picked })
        }}
      />
    </div>
  )
}