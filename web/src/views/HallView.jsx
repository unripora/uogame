import React, { useEffect, useMemo, useState } from 'react'
import { Search, Palette, Tag as TagIcon } from 'lucide-react'
import CategoryTabs from '../components/CategoryTabs'
import GameCard from '../components/GameCard'
import Empty from '../components/ui/Empty'
import Spinner from '../components/ui/Spinner'
import Select from '../components/ui/Select'
import TagPickerModal from '../components/TagPickerModal'
import { useGames } from '../hooks/useGames'
import { useCategories } from '../hooks/useCategories'
import { useTags } from '../hooks/useTags'
import useProgressiveList from '../hooks/useProgressiveList'
import { LS, lsGet } from '../hooks/usePersistence'
import { parseAliases } from '../lib/aliases'
import { getAllSources } from '../lib/source'
import { searchAllSources } from '../lib/sourceApi'

// 排序器：default 保持后端顺序（加入顺序），其余按对应规则
const SORTERS = {
  newest: (a, b) => (b.created_at || 0) - (a.created_at || 0),
  name: (a, b) => String(a.name || '').localeCompare(String(b.name || ''), 'zh'),
  ratingDesc: (a, b) =>
    (b.avg_rating || 0) - (a.avg_rating || 0) || (b.rating_count || 0) - (a.rating_count || 0),
  ratingAsc: (a, b) =>
    (a.avg_rating || 0) - (b.avg_rating || 0) || (b.rating_count || 0) - (a.rating_count || 0),
  sizeDesc: (a, b) => (b.size_bytes || 0) - (a.size_bytes || 0),
  sizeAsc: (a, b) => (a.size_bytes || 0) - (b.size_bytes || 0),
}

const SORT_OPTIONS = [
  ['default', '默认排序'],
  ['newest', '最新加入'],
  ['name', '名称'],
  ['ratingDesc', '评分：高 → 低'],
  ['ratingAsc', '评分：低 → 高'],
  ['sizeDesc', '大小：大 → 小'],
  ['sizeAsc', '大小：小 → 大'],
]

// 游戏大厅：搜索 + 分类/标签筛选 + 排序 + 最近游玩 + 游戏网格
export default function HallView({ onOpen, onPlay, onOpenAppearance }) {
  const { games, loading } = useGames()
  const { categories } = useCategories()
  const { tags } = useTags(games)
  const [activeCat, setActiveCat] = useState(0)
  const [filterTags, setFilterTags] = useState([])
  const [tagPickerOpen, setTagPickerOpen] = useState(false)
  const [sortBy, setSortBy] = useState('default')
  const [keyword, setKeyword] = useState('')

  // 跨源聚合搜索：仅当存在远程源且输入关键词时触发
  const [remoteItems, setRemoteItems] = useState([])
  const [remoteLoading, setRemoteLoading] = useState(false)
  const remoteSources = useMemo(
    () => getAllSources().filter((s) => !s.capabilities?.local),
    []
  )

  useEffect(() => {
    const kw = keyword.trim()
    if (!kw || remoteSources.length === 0) {
      setRemoteItems([])
      return
    }
    let alive = true
    setRemoteLoading(true)
    const timer = setTimeout(async () => {
      try {
        const { items } = await searchAllSources(remoteSources, kw)
        if (alive) setRemoteItems(items)
      } finally {
        if (alive) setRemoteLoading(false)
      }
    }, 300) // 防抖
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [keyword, remoteSources])

  const recentIds = lsGet(LS.recentGames, []) || []

  // 取游戏的分类 ID 列表（优先多值 category_ids，回退单值 category_id）
  const gameCatIds = (g) => {
    if (Array.isArray(g.category_ids) && g.category_ids.length) return g.category_ids
    return g.category_id ? [g.category_id] : []
  }

  const filtered = useMemo(() => {
    let list = games
    if (activeCat !== 0) {
      list = list.filter((g) => gameCatIds(g).some((id) => String(id) === String(activeCat)))
    }
    if (filterTags.length) {
      list = list.filter(
        (g) => Array.isArray(g.tags) && g.tags.some((t) => filterTags.includes(t))
      )
    }
    const kw = keyword.trim().toLowerCase()
    if (kw) {
      list = list.filter(
        (g) =>
          (g.name || '').toLowerCase().includes(kw) ||
          parseAliases(g.aliases).some((a) => (a || '').toLowerCase().includes(kw)) ||
          (g.tag || '').toLowerCase().includes(kw) ||
          (Array.isArray(g.tags) && g.tags.some((t) => (t || '').toLowerCase().includes(kw)))
      )
    }
    const sorter = SORTERS[sortBy]
    return sorter ? [...list].sort(sorter) : list
  }, [games, activeCat, filterTags, keyword, sortBy])

  const recent = useMemo(() => {
    if (!recentIds.length) return []
    return recentIds
      .map((id) => games.find((g) => String(g.id) === String(id)))
      .filter(Boolean)
      .slice(0, 6)
  }, [recentIds, games])

  // 渐进式渲染：筛选项/排序变化时重置；滚动到底部加载更多，避免一次性挂载数千卡片
  const { visible, sentinelRef, done } = useProgressiveList(filtered, {
    initial: 24,
    step: 24,
    deps: [activeCat, keyword, sortBy, filterTags.join(',')],
  })
  const shown = filtered.slice(0, visible)

  // 卡片点击：进入详情页（onOpen）；无 onOpen 时回退直接游玩
  const openGame = (g) => (onOpen ? onOpen(g) : onPlay && onPlay(g))

  return (
    <div className="p-4">
      <header className="mb-4 flex items-center justify-between">
        <div>
          <h1 className="text-lg font-black text-theme-text">拾游集</h1>
          <p className="text-xs text-theme-sub">共 {games.length} 款游戏</p>
        </div>
        <button
          onClick={onOpenAppearance}
          className="flex items-center gap-1 rounded-full bg-theme-panel px-3 py-1.5 text-xs font-bold text-theme-sub hover:text-theme-accent"
        >
          <Palette size={16} /> 外观
        </button>
      </header>

      <div className="relative mb-3">
        <Search
          size={16}
          className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-theme-sub"
        />
        <input
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          placeholder="搜索游戏名称或标签..."
          className="w-full rounded-xl border border-theme-border bg-theme-panel py-2 pl-9 pr-3 text-sm text-theme-text placeholder:text-theme-sub focus:border-theme-accent focus:outline-none"
        />
      </div>

      <div className="mb-4">
        <CategoryTabs
          categories={categories}
          active={activeCat}
          onChange={setActiveCat}
        />
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <button
          onClick={() => setTagPickerOpen(true)}
          className="flex items-center gap-1 rounded-full border border-theme-border bg-theme-panel px-3 py-1.5 text-xs font-bold text-theme-sub hover:text-theme-accent"
        >
          <TagIcon size={14} /> 标签筛选{filterTags.length ? `（${filterTags.length}）` : ''}
        </button>
        {filterTags.map((t) => (
          <button
            key={t}
            onClick={() => setFilterTags((p) => p.filter((x) => x !== t))}
            className="flex items-center gap-1 rounded-full bg-theme-accent px-3 py-1 text-xs font-bold text-white"
            title="移除"
          >
            {t} ✕
          </button>
        ))}
        {filterTags.length > 0 && (
          <button
            onClick={() => setFilterTags([])}
            className="text-xs font-bold text-theme-sub hover:text-theme-accent"
          >
            清除
          </button>
        )}
        <div className="ml-auto w-36">
          <Select value={sortBy} onChange={(e) => setSortBy(e.target.value)}>
            {SORT_OPTIONS.map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </Select>
        </div>
      </div>

      {recent.length > 0 && !keyword && activeCat === 0 && filterTags.length === 0 && (
        <section className="mb-5">
          <h2 className="mb-2 text-sm font-black text-theme-text">最近游玩</h2>
          <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
            {recent.map((g) => (
              <GameCard key={`recent-${g.id}`} game={g} onClick={openGame} />
            ))}
          </div>
        </section>
      )}

      {keyword.trim() && (remoteLoading || remoteItems.length > 0) && (
        <section className="mb-5">
          <h2 className="mb-2 flex items-center gap-2 text-sm font-black text-theme-text">
            来自其它源
            {remoteLoading && <Spinner />}
          </h2>
          {remoteItems.length === 0 ? (
            <p className="text-xs text-theme-sub">其它源暂无匹配结果</p>
          ) : (
            <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
              {remoteItems.map((g) => (
                <div key={`${g._source?.id}-${g.id}`} className="relative">
                  <GameCard game={g} onClick={openGame} />
                  <span className="pointer-events-none absolute left-1 top-1 rounded bg-black/60 px-1.5 py-0.5 text-[10px] text-white">
                    {g._source?.name}
                  </span>
                </div>
              ))}
            </div>
          )}
        </section>
      )}

      <section>
        <h2 className="mb-2 text-sm font-black text-theme-text">全部游戏</h2>
        {loading && games.length === 0 ? (
          <div className="flex justify-center py-12">
            <Spinner />
          </div>
        ) : filtered.length === 0 ? (
          <Empty text="没有找到相关游戏" />
        ) : (
          <>
            <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
              {shown.map((g) => (
                <GameCard key={g.id} game={g} onClick={openGame} />
              ))}
            </div>
            {/* 滚动哨兵：进入视口后自动加载下一批 */}
            {!done && (
              <div ref={sentinelRef} className="flex justify-center py-8">
                <Spinner />
              </div>
            )}
            {done && filtered.length > 24 && (
              <p className="py-6 text-center text-xs text-theme-sub">
                已显示全部 {filtered.length} 款游戏
              </p>
            )}
          </>
        )}
      </section>

      <TagPickerModal
        open={tagPickerOpen}
        title="标签筛选"
        allTags={tags}
        selected={filterTags}
        onClose={() => setTagPickerOpen(false)}
        onConfirm={(picked) => {
          setFilterTags(picked)
          setTagPickerOpen(false)
        }}
      />
    </div>
  )
}
