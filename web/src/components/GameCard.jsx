import React, { useState } from 'react'
import { Star } from 'lucide-react'
import { gameTypeLabel, platformTagColor } from '../lib/gameType'
import { usePlatformTagVisible } from '../hooks/usePublicSettings'
import { useCoverMap, resolveDefaultCover } from '../hooks/useDefaultCover'
import { parseAliases } from '../lib/aliases'

// 依名称生成一组稳定的渐变色（用于无封面占位）
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

// 游戏卡片：封面 + 名称 + 角标 + 类型 + 平台标签
// 封面优先级：真实封面 → 默认封面（分类映射/稳定随机）→ 渐变占位
export default function GameCard({ game, onClick }) {
  const showPlatformTag = usePlatformTagVisible()
  const coverMap = useCoverMap()
  const [imgError, setImgError] = useState(false)
  const [defaultError, setDefaultError] = useState(false)
  const cover =
    game.cover_url ||
    game.cover ||
    (game.id ? `/api/games/${game.id}/cover` : null)
  const hasCover = !!cover && !imgError
  const defaultCover = resolveDefaultCover(game, coverMap)
  const showPlaceholder = !hasCover && defaultError
  const aliases = parseAliases(game.aliases)

  return (
    <button
      onClick={() => onClick && onClick(game)}
      className="group flex flex-col overflow-hidden rounded-2xl border border-theme-border bg-theme-panel text-left transition hover:-translate-y-0.5 hover:shadow-lg"
    >
      <div className="relative aspect-[3/4] w-full overflow-hidden bg-black/10">
        {hasCover && (
          <img
            src={cover}
            alt={game.name}
            className="h-full w-full object-cover transition group-hover:scale-105"
            loading="lazy"
            onError={() => setImgError(true)}
          />
        )}
        {!hasCover && !defaultError && (
          <img
            src={defaultCover}
            alt={game.name}
            className="h-full w-full object-cover transition group-hover:scale-105"
            loading="lazy"
            onError={() => setDefaultError(true)}
          />
        )}
        {showPlaceholder && (
          <div
            className={[
              'flex h-full w-full items-center justify-center bg-gradient-to-br',
              gradientFor(game.name),
            ].join(' ')}
          >
            <span className="text-4xl font-black text-white/90 drop-shadow">
              {(game.name || '游').trim().charAt(0)}
            </span>
          </div>
        )}
        {game.badge && (
          <span
            className={[
              'absolute left-2 top-2 rounded-full px-2 py-0.5 text-[10px] font-black text-white',
              game.badge === 'HOT' ? 'bg-rose-500' : 'bg-emerald-500',
            ].join(' ')}
          >
            {game.badge}
          </span>
        )}
      </div>
      <div className="p-3">
        <div className="line-clamp-1 text-sm font-bold text-theme-text">
          {game.name}
        </div>
        {aliases.length > 0 && (
          <div className="mt-0.5 line-clamp-1 text-[10px] text-theme-sub" title={aliases.join('、')}>
            别名：{aliases.join('、')}
          </div>
        )}
        <div className="mt-1 flex items-center gap-1.5">
          <span className="line-clamp-1 text-[11px] text-theme-sub">
            {gameTypeLabel(game.game_type)}
          </span>
          {showPlatformTag && game.platform_tag && (
            <span
              className={[
                'shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-bold text-white',
                platformTagColor(game.platform_tag),
              ].join(' ')}
            >
              {game.platform_tag}
            </span>
          )}
          {game.rating_count > 0 && (
            <span className="ml-auto flex shrink-0 items-center gap-0.5 text-[11px] font-bold text-amber-400">
              <Star size={12} className="fill-amber-400" />
              {Number(game.avg_rating || 0).toFixed(1)}
              <span className="font-normal text-theme-sub">({game.rating_count})</span>
            </span>
          )}
        </div>
      </div>
    </button>
  )
}
