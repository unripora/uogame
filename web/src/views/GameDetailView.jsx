import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeft,
  Play,
  Star,
  ChevronLeft,
  ChevronRight,
  Tag as TagIcon,
  Calendar,
  HardDrive,
  ImageOff,
} from 'lucide-react'
import Spinner from '../components/ui/Spinner'
import { getJSON, getToken } from '../api/client'
import { API } from '../api/endpoints'
import { gameTypeLabel, platformTagColor, playKind } from '../lib/gameType'
import { parseAliases } from '../lib/aliases'
import { useCoverMap, resolveDefaultCover } from '../hooks/useDefaultCover'
import { useCategories } from '../hooks/useCategories'

// 依名称生成稳定渐变色（无图占位用），与 GameCard 保持一致观感
const GRADIENTS = [
  'from-sky-400 to-indigo-500',
  'from-emerald-400 to-teal-500',
  'from-amber-400 to-orange-500',
  'from-rose-400 to-pink-500',
  'from-violet-400 to-purple-500',
  'from-cyan-400 to-blue-500',
]
function gradientFor(name = '') {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0
  return GRADIENTS[h % GRADIENTS.length]
}

// 字节数格式化（与列表排序字段 size_bytes 对应）
function formatSize(bytes) {
  const n = Number(bytes) || 0
  if (n <= 0) return '未知'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

// 轮播图来源：优先真实封面（/api/games/:id/cover），其次挂载目录内的图片（/play/:id/<rel>）
// 说明：目录图片经 serveGameFile 提供，需带 token（与游玩 iframe 一致的鉴权通道）。
function buildImageList(game, dirImages) {
  const token = getToken()
  const tk = token ? `?token=${encodeURIComponent(token)}` : ''
  const list = []
  const cover = game.cover_url || game.cover || (game.id ? `/api/games/${game.id}/cover` : null)
  if (cover) list.push({ src: cover, kind: 'cover' })
  for (const rel of dirImages || []) {
    const url = `/play/${game.id}/${rel.split('/').map(encodeURIComponent).join('/')}${tk}`
    list.push({ src: url, kind: 'file', rel })
  }
  return list
}

// 游戏详情页：顶部滚动轮播 + 游戏信息 + 开始游玩
export default function GameDetailView({ game, onBack, onPlay }) {
  const coverMap = useCoverMap()
  const { categories } = useCategories()
  const [dirImages, setDirImages] = useState([])
  const [loadingImages, setLoadingImages] = useState(true)
  const [frame, setFrame] = useState(0)
  const [failed, setFailed] = useState({})
  const [coverFailed, setCoverFailed] = useState(false)
  const timerRef = useRef(null)

  const aliases = parseAliases(game.aliases)
  const kind = playKind(game.game_type)

  // 拉取目录内图片（优先封面，后端已排序）
  useEffect(() => {
    let alive = true
    setLoadingImages(true)
    setDirImages([])
    setFrame(0)
    setFailed({})
    setCoverFailed(false)
    getJSON(API.gameImages(game.id))
      .then((res) => {
        if (!alive) return
        const imgs = res && Array.isArray(res.images) ? res.images : []
        setDirImages(imgs)
      })
      .catch(() => {
        if (alive) setDirImages([])
      })
      .finally(() => {
        if (alive) setLoadingImages(false)
      })
    return () => {
      alive = false
    }
  }, [game.id])

  // 组装轮播图片列表（封面优先，其后为目录图片）
  const images = useMemo(() => {
    const list = buildImageList(game, dirImages)
    return list.filter((it) => !failed[it.src])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [game, dirImages, failed])

  const hasImages = images.length > 0
  const current = hasImages ? images[Math.min(frame, images.length - 1)] : null

  // 自动轮播：多于一张时每 4 秒切换
  useEffect(() => {
    if (images.length <= 1) return undefined
    timerRef.current = setInterval(() => {
      setFrame((f) => (f + 1) % images.length)
    }, 4000)
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [images.length])

  const go = (delta) => {
    if (!hasImages) return
    setFrame((f) => (f + delta + images.length) % images.length)
  }

  // 分类名
  const categoryNames = useMemo(() => {
    const ids = Array.isArray(game.category_ids) && game.category_ids.length
      ? game.category_ids
      : game.category_id
        ? [game.category_id]
        : []
    return ids
      .map((id) => (categories || []).find((c) => String(c.id) === String(id)))
      .filter(Boolean)
      .map((c) => c.name)
  }, [game, categories])

  const defaultCover = resolveDefaultCover(game, coverMap)
  const showCoverFallback = !hasImages || (current && current.kind === 'cover' && coverFailed)

  return (
    <div className="min-h-full bg-theme-bg pb-24">
      {/* 顶部轮播 */}
      <div className="relative w-full overflow-hidden bg-black/30">
        <div className="relative aspect-[16/9] w-full sm:aspect-[21/9]">
          {loadingImages && !hasImages ? (
            <div className="flex h-full w-full items-center justify-center">
              <Spinner />
            </div>
          ) : hasImages && !coverFailed ? (
            images.map((it, i) => (
              <img
                key={it.src}
                src={it.src}
                alt={game.name}
                draggable={false}
                className={[
                  'absolute inset-0 h-full w-full object-cover transition-opacity duration-500',
                  i === (current ? images.indexOf(current) : 0) ? 'opacity-100' : 'opacity-0',
                ].join(' ')}
                onError={() => {
                  if (it.kind === 'cover') setCoverFailed(true)
                  setFailed((prev) => ({ ...prev, [it.src]: true }))
                }}
              />
            ))
          ) : (
            <img
              src={defaultCover}
              alt={game.name}
              className="absolute inset-0 h-full w-full object-cover"
              onError={() =>
                setFailed((prev) => ({ ...prev, __def: true }))
              }
            />
          )}
          {showCoverFallback && failed.__def && (
            <div
              className={[
                'absolute inset-0 flex items-center justify-center bg-gradient-to-br',
                gradientFor(game.name),
              ].join(' ')}
            >
              <span className="text-6xl font-black text-white/90 drop-shadow">
                {(game.name || '游').trim().charAt(0)}
              </span>
            </div>
          )}

          {/* 渐变蒙层，保证文字可读 */}
          <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/70 via-black/10 to-black/20" />

          {/* 返回按钮 */}
          <button
            onClick={onBack}
            className="absolute left-3 top-3 flex items-center gap-1 rounded-full bg-black/45 px-3 py-1.5 text-xs font-bold text-white backdrop-blur hover:bg-black/65"
          >
            <ArrowLeft size={16} /> 返回
          </button>

          {/* 轮播控制 */}
          {images.length > 1 && (
            <>
              <button
                onClick={() => go(-1)}
                className="absolute left-3 top-1/2 flex h-9 w-9 -translate-y-1/2 items-center justify-center rounded-full bg-black/40 text-white backdrop-blur hover:bg-black/60"
                title="上一张"
              >
                <ChevronLeft size={20} />
              </button>
              <button
                onClick={() => go(1)}
                className="absolute right-3 top-1/2 flex h-9 w-9 -translate-y-1/2 items-center justify-center rounded-full bg-black/40 text-white backdrop-blur hover:bg-black/60"
                title="下一张"
              >
                <ChevronRight size={20} />
              </button>
              <div className="absolute bottom-3 left-1/2 flex -translate-x-1/2 gap-1.5">
                {images.map((it, i) => (
                  <button
                    key={it.src}
                    onClick={() => setFrame(i)}
                    className={[
                      'h-1.5 rounded-full transition-all',
                      i === (current ? images.indexOf(current) : 0)
                        ? 'w-5 bg-white'
                        : 'w-1.5 bg-white/50 hover:bg-white/80',
                    ].join(' ')}
                    title={`第 ${i + 1} 张`}
                  />
                ))}
              </div>
            </>
          )}

          {/* 轮播无图提示（仅调试可见） */}
          {!loadingImages && dirImages.length === 0 && images.length <= 1 && (
            <span className="absolute bottom-3 right-3 flex items-center gap-1 rounded-full bg-black/40 px-2 py-0.5 text-[10px] text-white/70">
              <ImageOff size={12} /> 无更多图片
            </span>
          )}
        </div>
      </div>

      {/* 游戏信息 */}
      <div className="mx-auto -mt-8 max-w-3xl px-4">
        <div className="rounded-2xl border border-theme-border bg-theme-panel p-4 shadow-lg">
          <div className="flex items-start gap-4">
            <div className="relative h-24 w-18 shrink-0 overflow-hidden rounded-xl bg-black/10" style={{ width: '4.5rem' }}>
              {!coverFailed ? (
                <img
                  src={game.cover_url || game.cover || (game.id ? `/api/games/${game.id}/cover` : defaultCover)}
                  alt={game.name}
                  className="h-full w-full object-cover"
                  onError={() => setCoverFailed(true)}
                />
              ) : (
                <div
                  className={[
                    'flex h-full w-full items-center justify-center bg-gradient-to-br',
                    gradientFor(game.name),
                  ].join(' ')}
                >
                  <span className="text-2xl font-black text-white/90">
                    {(game.name || '游').trim().charAt(0)}
                  </span>
                </div>
              )}
            </div>

            <div className="min-w-0 flex-1">
              <h1 className="line-clamp-2 text-xl font-black text-theme-text">{game.name}</h1>
              {aliases.length > 0 && (
                <div className="mt-0.5 text-xs text-theme-sub" title={aliases.join('、')}>
                  别名：{aliases.join('、')}
                </div>
              )}
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <span className="rounded-full bg-theme-bg px-2 py-0.5 text-[11px] font-bold text-theme-sub">
                  {gameTypeLabel(game.game_type)}
                </span>
                {game.platform_tag && (
                  <span
                    className={[
                      'rounded-full px-2 py-0.5 text-[11px] font-bold text-white',
                      platformTagColor(game.platform_tag),
                    ].join(' ')}
                  >
                    {game.platform_tag}
                  </span>
                )}
                {game.rating_count > 0 && (
                  <span className="flex items-center gap-0.5 text-xs font-bold text-amber-400">
                    <Star size={13} className="fill-amber-400" />
                    {Number(game.avg_rating || 0).toFixed(1)}
                    <span className="font-normal text-theme-sub">({game.rating_count})</span>
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* 开始游玩 */}
          <button
            onClick={() => onPlay && onPlay(game)}
            className="mt-4 flex w-full items-center justify-center gap-2 rounded-xl bg-theme-accent py-3 text-sm font-black text-white transition hover:brightness-110"
          >
            <Play size={18} className="fill-white" /> 开始游玩
          </button>
        </div>

        {/* 详情区块 */}
        <div className="mt-4 space-y-4">
          {(game.description || '').trim() ? (
            <section className="rounded-2xl border border-theme-border bg-theme-panel p-4">
              <h2 className="mb-2 text-sm font-black text-theme-text">游戏简介</h2>
              <p className="whitespace-pre-wrap text-sm leading-relaxed text-theme-sub">
                {game.description}
              </p>
            </section>
          ) : null}

          <section className="rounded-2xl border border-theme-border bg-theme-panel p-4">
            <h2 className="mb-3 text-sm font-black text-theme-text">详细信息</h2>
            <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-2">
              <InfoRow icon={<TagIcon size={14} />} label="类型" value={gameTypeLabel(game.game_type)} />
              <InfoRow icon={<HardDrive size={14} />} label="大小" value={formatSize(game.size_bytes)} />
              {game.release_date && (
                <InfoRow icon={<Calendar size={14} />} label="发行日期" value={game.release_date} />
              )}
              {categoryNames.length > 0 && (
                <InfoRow icon={<TagIcon size={14} />} label="分类" value={categoryNames.join('、')} />
              )}
              {Array.isArray(game.tags) && game.tags.length > 0 && (
                <InfoRow icon={<TagIcon size={14} />} label="标签" value={game.tags.join('、')} />
              )}
              {game.external_source && (
                <InfoRow icon={<TagIcon size={14} />} label="元数据来源" value={game.external_source} />
              )}
            </dl>
          </section>
        </div>

        {/* 备注：ROM / Flash 由对应内核承载，此处仅作提示 */}
        {kind !== 'h5' && (
          <p className="mt-3 text-center text-xs text-theme-sub">
            该游戏将使用模拟器内核运行（{gameTypeLabel(game.game_type)}）
          </p>
        )}
      </div>
    </div>
  )
}

function InfoRow({ icon, label, value }) {
  return (
    <div className="flex items-center gap-2">
      <span className="flex items-center gap-1 text-theme-sub">{icon}{label}</span>
      <span className="ml-auto truncate font-bold text-theme-text" title={String(value)}>
        {value}
      </span>
    </div>
  )
}
