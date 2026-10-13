import React, { useCallback, useEffect, useState } from 'react'
import { Trash2, Pencil, Download, Tags, Sparkles, ChevronLeft, ChevronRight } from 'lucide-react'
import Button from '../../components/ui/Button'
import Input from '../../components/ui/Input'
import Select from '../../components/ui/Select'
import Modal from '../../components/ui/Modal'
import Spinner from '../../components/ui/Spinner'
import { useCategories } from '../../hooks/useCategories'
import { useToast } from '../../contexts/ToastContext'
import { API } from '../../api/endpoints'
import { del, putJSON, postJSON, getJSON } from '../../api/client'
import { GAME_TYPES, gameTypeLabel } from '../../lib/gameType'
import { parseAliases } from '../../lib/aliases'

// 每页可选条数
const PAGE_SIZES = [20, 50, 100, 200]

// 游戏管理面板：后端分页 + 选择 / 批量分类 / 批量导出 / 批量删除 / 编辑
// 说明：3000+ 游戏下改为**服务端分页**（GET /api/games?page&page_size&q），
// 仅拉取当前页数据，避免一次性返回与渲染整表导致的卡顿。
export default function GameManagePanel() {
  const { categories } = useCategories()
  const toast = useToast()
  const [selected, setSelected] = useState([])
  const [keyword, setKeyword] = useState('')
  const [debouncedKw, setDebouncedKw] = useState('')
  const [editing, setEditing] = useState(null)
  const [batchCat, setBatchCat] = useState('')

  // 分页状态
  const [items, setItems] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [loading, setLoading] = useState(false)

  // 关键词防抖（避免每次按键都请求）
  useEffect(() => {
    const t = setTimeout(() => {
      setDebouncedKw(keyword.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(t)
  }, [keyword])

  // 拉取当前页
  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getJSON(API.gamesPaged({ page, pageSize, q: debouncedKw }))
      if (res && Array.isArray(res.items)) {
        setItems(res.items)
        setTotal(Number(res.total) || 0)
      } else if (Array.isArray(res)) {
        // 兼容旧全量契约（理论上不会走到）
        setItems(res)
        setTotal(res.length)
      }
    } catch (e) {
      toast.error(e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, debouncedKw, toast])

  useEffect(() => {
    load()
  }, [load])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  // 当前页数据变更后清理越界的选中项
  const pageIds = items.map((g) => g.id)

  const reload = async () => {
    // 删除/改动后若当前页被清空则回退一页
    if (items.length === 0 && page > 1) setPage((p) => Math.max(1, p - 1))
    else await load()
  }

  const toggle = (id) => {
    setSelected((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    )
  }

  const toggleAll = () => {
    if (pageIds.length > 0 && pageIds.every((id) => selected.includes(id))) {
      setSelected((prev) => prev.filter((id) => !pageIds.includes(id)))
    } else {
      setSelected((prev) => Array.from(new Set([...prev, ...pageIds])))
    }
  }

  const allOnPageSelected = pageIds.length > 0 && pageIds.every((id) => selected.includes(id))

  const batchSetCategory = async () => {
    if (!batchCat) {
      toast.error('请先选择一个目标分类')
      return
    }
    try {
      await postJSON(API.games + '/batch-category', {
        ids: selected,
        category_id: Number(batchCat),
      })
      toast.success('批量分类修改成功')
      setSelected([])
      await reload()
    } catch (e) {
      toast.error(e.message || '批量修改失败')
    }
  }

  const batchDelete = async () => {
    if (!confirm(`危险操作：这将会彻底删除所选的 ${selected.length} 个游戏，确定继续吗？`))
      return
    try {
      await postJSON(API.games + '/batch-delete', { ids: selected })
      toast.success('批量删除成功')
      setSelected([])
      await reload()
    } catch (e) {
      toast.error(e.message || '批量删除失败')
    }
  }

  const batchExport = async () => {
    if (!confirm(`确认导出所选的 ${selected.length} 款游戏？`)) return
    try {
      const res = await postJSON(API.gamesBatchExport, { ids: selected })
      toast.success(`成功导出 ${selected.length} 款游戏！`)
      void res
    } catch (e) {
      toast.error(e.message || '批量导出失败，请稍后重试')
    }
  }

  const removeOne = async (g) => {
    if (!confirm('危险操作：这将会从数据库中移除该游戏，确定继续吗？')) return
    try {
      await del(API.game(g.id))
      toast.success('游戏已删除')
      await reload()
    } catch (e) {
      toast.error(e.message || '操作失败')
    }
  }

  // 刮削元数据/封面：调用后端 provider 体系，展示命中结果
  const [scrapingId, setScrapingId] = useState(null)
  const scrapeOne = async (g) => {
    setScrapingId(g.id)
    try {
      const res = await postJSON(API.gameScrape(g.id))
      const r = res?.result || {}
      if (r.missed) {
        toast.error('未命中：该游戏暂无可用元数据')
      } else {
        const parts = []
        if (r.cover_set) parts.push('封面')
        if (r.description) parts.push('简介')
        if (r.release_date) parts.push('发行日期')
        toast.success(`刮削成功：${parts.join('、') || '元数据已更新'}`)
        await reload()
      }
    } catch (e) {
      toast.error(e.message || '刮削失败')
    } finally {
      setScrapingId(null)
    }
  }

  const saveEdit = async () => {
    try {
      await putJSON(API.game(editing.id), {
        name: editing.name,
        game_type: editing.game_type,
        category_id: Number((editing.category_ids || [])[0]) || Number(editing.category_id) || 0,
        category_ids: (editing.category_ids || []).map(Number),
        tags: editing.tags || [],
        sort_order: Number(editing.sort_order) || 0,
        badge: editing.badge || '',
        hidden: !!editing.hidden,
      })
      toast.success('保存修改')
      setEditing(null)
      await reload()
    } catch (e) {
      toast.error(e.message || '保存失败')
    }
  }

  // 打开编辑：把多值字段规范化为数组
  const openEdit = (g) => {
    const catIds = Array.isArray(g.category_ids) && g.category_ids.length
      ? g.category_ids
      : g.category_id ? [g.category_id] : []
    const tags = Array.isArray(g.tags) ? g.tags : []
    setEditing({ ...g, category_ids: catIds, tags, tagInput: '' })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-black text-theme-text">游戏管理</h2>
        <span className="text-xs text-theme-sub">共 {total} 款</span>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="max-w-xs"
          placeholder="搜索游戏名称或别名..."
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
        />
        <Button variant="ghost" onClick={toggleAll}>
          {allOnPageSelected ? '取消本页全选' : '本页全选'}
        </Button>
        <span className="text-xs text-theme-sub">已选择 {selected.length} 项</span>
      </div>

      {selected.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-xl border border-theme-border bg-theme-panel p-2">
          <Select
            className="max-w-[180px]"
            value={batchCat}
            onChange={(e) => setBatchCat(e.target.value)}
          >
            <option value="">请选择目标分类...</option>
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </Select>
          <Button onClick={batchSetCategory}>
            <Tags size={14} /> 批量修改分类
          </Button>
          <Button variant="ghost" onClick={batchExport}>
            <Download size={14} /> 导出
          </Button>
          <Button variant="danger" onClick={batchDelete}>
            <Trash2 size={14} /> 删除
          </Button>
        </div>
      )}

      {loading && items.length === 0 ? (
        <div className="flex justify-center py-8">
          <Spinner />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-xl border border-theme-border py-10 text-center text-sm text-theme-sub">
          {debouncedKw ? '没有找到匹配的游戏' : '暂无游戏'}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-xl border border-theme-border">
          <table className="w-full text-left text-sm">
            <thead className="bg-theme-panel text-[11px] text-theme-sub">
              <tr>
                <th className="p-2"></th>
                <th className="p-2">名称</th>
                <th className="p-2">类型</th>
                <th className="p-2">分类</th>
                <th className="p-2">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((g) => (
                <tr key={g.id} className="border-t border-theme-border">
                  <td className="p-2">
                    <input
                      type="checkbox"
                      checked={selected.includes(g.id)}
                      onChange={() => toggle(g.id)}
                    />
                  </td>
                  <td className="p-2 font-bold text-theme-text">
                    <div>{g.name}</div>
                    {parseAliases(g.aliases).length > 0 && (
                      <div className="text-[10px] font-normal text-theme-sub">
                        别名：{parseAliases(g.aliases).join('、')}
                      </div>
                    )}
                  </td>
                  <td className="p-2 text-theme-sub">{gameTypeLabel(g.game_type)}</td>
                  <td className="p-2 text-theme-sub">{g.category_name || g.category_id || '默认分类'}</td>
                  <td className="p-2">
                    <div className="flex gap-2">
                      <button
                        className="text-theme-sub hover:text-theme-accent disabled:opacity-40"
                        onClick={() => scrapeOne(g)}
                        disabled={scrapingId === g.id}
                        title="刮削元数据/封面"
                      >
                        <Sparkles size={16} />
                      </button>
                      <button
                        className="text-theme-sub hover:text-theme-accent"
                        onClick={() => openEdit(g)}
                        title="编辑"
                      >
                        <Pencil size={16} />
                      </button>
                      <button
                        className="text-theme-sub hover:text-rose-500"
                        onClick={() => removeOne(g)}
                        title="删除"
                      >
                        <Trash2 size={16} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* 分页器 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-xs text-theme-sub">
          <span>每页</span>
          <select
            className="rounded-lg border border-theme-border bg-theme-panel px-2 py-1 text-theme-text"
            value={pageSize}
            onChange={(e) => {
              setPageSize(Number(e.target.value))
              setPage(1)
            }}
          >
            {PAGE_SIZES.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          <span>条</span>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            disabled={page <= 1 || loading}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            <ChevronLeft size={14} /> 上一页
          </Button>
          <span className="text-xs text-theme-sub">
            第 {page} / {totalPages} 页
          </span>
          <Button
            variant="ghost"
            disabled={page >= totalPages || loading}
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
          >
            下一页 <ChevronRight size={14} />
          </Button>
        </div>
      </div>

      {editing && (
        <Modal
          open
          title="编辑配置"
          onClose={() => setEditing(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setEditing(null)}>
                取消
              </Button>
              <Button onClick={saveEdit}>保存修改</Button>
            </>
          }
        >
          <div className="space-y-3">
            <Input
              placeholder="游戏名称"
              value={editing.name || ''}
              onChange={(e) => setEditing({ ...editing, name: e.target.value })}
            />
            <Select
              value={editing.game_type || 'H5'}
              onChange={(e) => setEditing({ ...editing, game_type: e.target.value })}
            >
              {GAME_TYPES.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </Select>
            <div>
              <div className="mb-1 text-xs font-bold text-theme-sub">分类（可多选）</div>
              <div className="flex flex-wrap gap-2">
                {categories.length === 0 && (
                  <span className="text-xs text-theme-sub">暂无分类</span>
                )}
                {categories.map((c) => {
                  const checked = (editing.category_ids || []).some(
                    (id) => String(id) === String(c.id)
                  )
                  return (
                    <label
                      key={c.id}
                      className={[
                        'cursor-pointer rounded-full border px-3 py-1 text-xs font-bold transition',
                        checked
                          ? 'border-theme-accent bg-theme-accent text-white'
                          : 'border-theme-border bg-theme-panel text-theme-sub hover:text-theme-accent',
                      ].join(' ')}
                    >
                      <input
                        type="checkbox"
                        className="hidden"
                        checked={checked}
                        onChange={() => {
                          const cur = editing.category_ids || []
                          const next = checked
                            ? cur.filter((id) => String(id) !== String(c.id))
                            : [...cur, c.id]
                          setEditing({ ...editing, category_ids: next })
                        }}
                      />
                      {c.name}
                    </label>
                  )
                })}
              </div>
            </div>

            <div>
              <div className="mb-1 text-xs font-bold text-theme-sub">标签（回车添加，可多个）</div>
              <div className="mb-2 flex flex-wrap gap-2">
                {(editing.tags || []).map((t) => (
                  <span
                    key={t}
                    className="flex items-center gap-1 rounded-full bg-theme-accent/15 px-2 py-0.5 text-xs font-bold text-theme-accent"
                  >
                    {t}
                    <button
                      type="button"
                      className="text-theme-sub hover:text-rose-500"
                      onClick={() =>
                        setEditing({
                          ...editing,
                          tags: (editing.tags || []).filter((x) => x !== t),
                        })
                      }
                    >
                      ×
                    </button>
                  </span>
                ))}
              </div>
              <Input
                placeholder="输入标签后按回车"
                value={editing.tagInput || ''}
                onChange={(e) => setEditing({ ...editing, tagInput: e.target.value })}
                onKeyDown={(e) => {
                  if (e.key !== 'Enter') return
                  e.preventDefault()
                  const v = (editing.tagInput || '').trim()
                  if (!v) return
                  const cur = editing.tags || []
                  if (cur.includes(v)) {
                    setEditing({ ...editing, tagInput: '' })
                    return
                  }
                  setEditing({ ...editing, tags: [...cur, v], tagInput: '' })
                }}
              />
            </div>

            <Input
              type="number"
              placeholder="排序权重"
              value={editing.sort_order || 0}
              onChange={(e) => setEditing({ ...editing, sort_order: e.target.value })}
            />
            <Select
              value={editing.badge || ''}
              onChange={(e) => setEditing({ ...editing, badge: e.target.value })}
            >
              <option value="">无角标</option>
              <option value="HOT">HOT (热门)</option>
              <option value="NEW">NEW (新品)</option>
            </Select>
            <label className="flex items-center gap-2 text-sm text-theme-text">
              <input
                type="checkbox"
                checked={!!editing.hidden}
                onChange={(e) => setEditing({ ...editing, hidden: e.target.checked })}
              />
              隐藏下架
            </label>
          </div>
        </Modal>
      )}
    </div>
  )
}
