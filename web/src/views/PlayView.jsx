import React, { useEffect, useRef, useState } from 'react'
import { ArrowLeft, Maximize2, Minimize2, RotateCw, RotateCcw, Home, Star } from 'lucide-react'
import { playKind } from '../lib/gameType'
import { getJSON, postJSON, getToken, resolveUrl } from '../api/client'
import { API } from '../api/endpoints'
import { getCurrentSource, getSourceById, resolveSourceUrl } from '../lib/source'
import Spinner from '../components/ui/Spinner'
import FloatingBall from '../components/FloatingBall'
import { useToast } from '../contexts/ToastContext'
import useGamepad, { PAD_BUTTON } from '../hooks/useGamepad'
import { useRatings } from '../hooks/useRatings'

// 游玩视图：根据 game_type 加载 H5 / 模拟器 / Flash
// 契约（与后端 router 一致，必须保持）：
//   H5    → iframe src = /play/{id}/index.html
//   ROM   → iframe src = /emulator/emulatorjs.html  （内含 /play/{id}/{rom} 与 /api/games/{id}/roms）
//   FLASH → iframe src = /emulator/ruffle.html

// 倍速档位（H5 适用：基于 rAF / 时间源的引擎可生效，其它引擎不保证）
const SPEED_PRESETS = [0.5, 1, 1.5, 2, 3, 4]

// 屏幕方向锁定（尽力而为）：移动端浏览器支持则按当前方向锁定，桌面端不支持时静默忽略
// 注意：必须在「进入全屏」的用户手势调用栈内（或紧随 requestFullscreen 的 Promise 之后）调用才有效，
// 放到异步的 fullscreenchange 事件里会因脱离手势上下文而被浏览器拒绝。
function lockOrientation(orient) {
  const so = window.screen && window.screen.orientation
  if (!so || typeof so.lock !== 'function') return Promise.resolve(false)
  try {
    const p = so.lock(orient === 'landscape' ? 'landscape' : 'portrait')
    if (p && typeof p.then === 'function') {
      return p.then(() => true).catch(() => false)
    }
    return Promise.resolve(true)
  } catch {
    return Promise.resolve(false)
  }
}
function unlockOrientation() {
  const so = window.screen && window.screen.orientation
  if (!so || typeof so.unlock !== 'function') return
  try {
    so.unlock()
  } catch {
    /* ignore */
  }
}

export default function PlayView({ game, onExit }) {
  const toast = useToast()
  const kind = playKind(game.game_type)
  const [src, setSrc] = useState('')
  const [loading, setLoading] = useState(true)
  const [refreshKey, setRefreshKey] = useState(0)
  const frameRef = useRef(null)
  const wrapRef = useRef(null)

  // 倍速：按游戏记忆，默认 1x；仅 H5 生效（经 iframe 内注入的 __uogameSetSpeed 应用）
  const [speed, setSpeed] = useState(() => {
    try {
      const s = Number(localStorage.getItem(`uogame_speed_${game.id}`))
      if (s > 0) return s
    } catch {
      /* ignore */
    }
    return 1
  })
  const speedRef = useRef(speed)
  useEffect(() => {
    speedRef.current = speed
  }, [speed])

  // 屏幕方向：默认取后台的 is_landscape 标记，用户切换后按游戏记忆
  const [orient, setOrient] = useState(() => {
    try {
      const saved = localStorage.getItem(`uogame_orient_${game.id}`)
      if (saved === 'landscape' || saved === 'portrait') return saved
    } catch {
      /* ignore */
    }
    return game.is_landscape ? 'landscape' : 'portrait'
  })
  const switchOrient = () => {
    const next = orient === 'portrait' ? 'landscape' : 'portrait'
    setOrient(next)
    // 切换方向时退出伪全屏，避免竖屏伪全屏与横屏真全屏状态串味
    setPseudoFs(false)
    try {
      localStorage.setItem(`uogame_orient_${game.id}`, next)
    } catch {
      /* ignore */
    }
  }

  // 倍速应用到游戏 iframe（同源，直接调用注入脚本暴露的 __uogameSetSpeed）
  const pushSpeed = (s) => {
    const win = frameRef.current && frameRef.current.contentWindow
    if (win && typeof win.__uogameSetSpeed === 'function') {
      try {
        win.__uogameSetSpeed(s)
      } catch {
        /* ignore */
      }
    }
  }
  const applySpeed = (s) => {
    setSpeed(s)
    try {
      localStorage.setItem(`uogame_speed_${game.id}`, String(s))
    } catch {
      /* ignore */
    }
    pushSpeed(s)
  }

  // 评分：我的评分（一人一票可改）+ 综合评分（平台聚合）
  const { mine, rate, unrate } = useRatings()
  const [agg, setAgg] = useState({
    avg_rating: game.avg_rating || 0,
    rating_count: game.rating_count || 0,
  })
  const myScore = mine[String(game.id)] || 0

  const onRate = async (score) => {
    try {
      const res = await rate(game.id, score)
      setAgg({ avg_rating: res.avg_rating, rating_count: res.rating_count })
      toast.success(`已评 ${score} 星`)
    } catch (e) {
      toast.error(e.message || '评分失败')
    }
  }
  const onUnrate = async () => {
    try {
      const res = await unrate(game.id)
      setAgg({ avg_rating: res.avg_rating, rating_count: res.rating_count })
      toast.success('已取消评分')
    } catch (e) {
      toast.error(e.message || '取消失败')
    }
  }

  // 构造 iframe 地址；ROM 需先探测可用 ROM 列表
  useEffect(() => {
    let alive = true
    ;(async () => {
      setLoading(true)
      setSrc('')
      // iframe 内为跨文档上下文，Cookie 可能因 SameSite 不发送，统一补 ?token=
      const token = getToken()
      const tokenQuery = token ? `&token=${encodeURIComponent(token)}` : ''
      try {
        if (kind === 'h5') {
          if (game._static) {
            // 静态源游戏：直连源地址（GitHub Pages），不经后端
            // 播放地址 = baseUrl + path + entry
            const src = getSourceById(game._source?.id) || getCurrentSource()
            const path = `${game.path || ''}${game.entry || 'index.html'}`
            if (alive) setSrc(resolveSourceUrl(src, path))
          } else {
            // H5 游戏本体随源下发：按当前源解析播放地址（本地源=同源相对路径）
            const playPath = `/play/${game.id}/index.html${
              token ? `?token=${encodeURIComponent(token)}` : ''
            }`
            if (alive) setSrc(resolveUrl(playPath))
          }
        } else if (kind === 'flash') {
          // Flash 通过 ruffle.html 加载，gameId 参数 + token 告知目标
          // 播放器页为前端本地资产（同源），其中的游戏文件地址由源决定
          if (alive)
            setSrc(`/emulator/ruffle.html?gameId=${game.id}${tokenQuery}`)
        } else {
          const roms = await getJSON(API.gameRoms(game.id))
          const list = Array.isArray(roms) ? roms : []
          if (!list.length) {
            toast.error('未找到可用的游戏文件')
            if (alive) setLoading(false)
            return
          }
          const rom = list[0]
          // 传 type 告知模拟器页选用核心（缺省会被当作 GBA）
          // 模拟器页同时需要知道游戏文件所在的源地址（跨源时）
          const sourceBase = resolveSourceUrl(getCurrentSource(), '/').replace(/\/$/, '')
          const url = `/emulator/emulatorjs.html?gameId=${game.id}&rom=${encodeURIComponent(
            rom
          )}&type=${encodeURIComponent(game.game_type || '')}${
            sourceBase ? `&source=${encodeURIComponent(sourceBase)}` : ''
          }${tokenQuery}`
          if (alive) setSrc(url)
        }
      } catch {
        toast.error('游戏文件加载失败')
      } finally {
        if (alive) setLoading(false)
      }
    })()
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [game.id, kind, refreshKey])

  // 记录最近游玩（仅本地源，静态源游戏 id 可能冲突，不污染）
  useEffect(() => {
    if (game._static) return
    try {
      const key = 'uogame_recent_games_list'
      const cur = JSON.parse(localStorage.getItem(key) || '[]')
      const next = [game.id, ...cur.filter((x) => String(x) !== String(game.id))].slice(
        0,
        12
      )
      localStorage.setItem(key, JSON.stringify(next))
    } catch {
      /* ignore */
    }
  }, [game.id, game._static])

  // 上报游玩时长（简单心跳：进入 + 离开时结算）
  // 静态源游戏无本地 game_id，跳过上报（排行榜仅本地源计入）
  useEffect(() => {
    if (game._static) return
    const started = Date.now()
    const report = () => {
      const secs = Math.max(1, Math.round((Date.now() - started) / 1000))
      postJSON(API.userPlaytime, { game_id: game.id, duration: secs }).catch(() => {})
    }
    window.addEventListener('beforeunload', report)
    return () => {
      window.removeEventListener('beforeunload', report)
      report()
    }
  }, [game.id, game._static])

  // 全屏：进入 / 退出切换（对 iframe 外框生效，方向比例保持不变）
  const [isFs, setIsFs] = useState(false)
  // 伪全屏：不调用系统全屏（避免安卓屏幕被强制旋转），仅用 CSS 把游玩区铺满视口
  const [pseudoFs, setPseudoFs] = useState(false)
  const orientRef = useRef(orient)
  useEffect(() => {
    orientRef.current = orient
  }, [orient])

  // 全屏期间锁定方向的重试定时器（部分 Android 浏览器需在全屏后稍等片刻才能 lock 成功）
  const fsLockTimer = useRef(null)

  useEffect(() => {
    const on = () => {
      const entered = !!document.fullscreenElement
      setIsFs(entered)
      if (!entered) {
        // 退出全屏时解除方向锁定，并停止重试
        if (fsLockTimer.current) {
          clearInterval(fsLockTimer.current)
          fsLockTimer.current = null
        }
        unlockOrientation()
      }
    }
    document.addEventListener('fullscreenchange', on)
    return () => {
      document.removeEventListener('fullscreenchange', on)
      if (fsLockTimer.current) clearInterval(fsLockTimer.current)
      unlockOrientation()
    }
  }, [])

  // 进入全屏后按当前方向锁定屏幕旋转：立即尝试，失败则短暂重试（覆盖 Android 全屏过渡期拒绝 lock 的情况）
  // —— 仅在走「系统全屏」时使用；伪全屏不旋转屏幕，无需锁定
  const startOrientationLock = (orient) => {
    if (fsLockTimer.current) clearInterval(fsLockTimer.current)
    let tries = 0
    const attempt = async () => {
      const ok = await lockOrientation(orient)
      tries += 1
      // 成功即停；最多重试 8 次（约 2 秒）
      if (ok || tries >= 8) {
        if (fsLockTimer.current) {
          clearInterval(fsLockTimer.current)
          fsLockTimer.current = null
        }
      }
    }
    attempt()
    fsLockTimer.current = setInterval(attempt, 250)
  }

  // 按当前方向选择全屏方式：
  // - 竖屏：伪全屏（不调用系统全屏，避免安卓屏幕被强制旋转，游戏保持竖显）
  // - 横屏：真全屏（调用系统全屏并锁定横屏）
  const toggleFullscreen = () => {
    if (orientRef.current === 'landscape') {
      nativeFullscreen()
    } else {
      setPseudoFs((v) => !v)
    }
  }

  // 伪全屏期间：锁定背景滚动 + 支持 Esc 退出
  useEffect(() => {
    if (!pseudoFs) return
    const onKey = (e) => {
      if (e.key === 'Escape') setPseudoFs(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [pseudoFs])

  // 走系统全屏（横屏用）：调用 requestFullscreen 并尝试锁定横屏
  function nativeFullscreen() {
    if (document.fullscreenElement) {
      if (fsLockTimer.current) clearInterval(fsLockTimer.current)
      fsLockTimer.current = null
      unlockOrientation()
      if (document.exitFullscreen) document.exitFullscreen()
      return
    }
    const el = kind === 'h5' && wrapRef.current ? wrapRef.current : frameRef.current
    if (!el || !el.requestFullscreen) return
    const orient = orientRef.current
    const fp = el.requestFullscreen()
    if (fp && typeof fp.then === 'function') {
      fp.then(() => startOrientationLock(orient)).catch(() => startOrientationLock(orient))
    } else {
      startOrientationLock(orient)
    }
  }

  // ---- 游戏内手柄处理 ----
  // 1) 全局静音（useGamepad enabled=false），避免 D-pad/面键误触返回
  // 2) 退出确认弹窗打开时启用白名单（仅 A/B/Back），可手柄操作弹窗
  // 3) ROM/Flash：采用原生手柄直读（EmulatorJS/Ruffle 自行处理），仅监听 Select+Start 长按退出
  const [exitConfirm, setExitConfirm] = useState(false)
  const chordRef = useRef({ since: 0, fired: false })

  // Select(8)+Start(9) 同时长按 1.5s → 呼出退出确认（ROM/Flash 通用逃生手势）
  useEffect(() => {
    if (kind === 'h5') return // H5 由全局模态处理，无需 chord
    let raf = 0
    const tick = (now) => {
      raf = requestAnimationFrame(tick)
      const pads = navigator.getGamepads ? navigator.getGamepads() : []
      let both = false
      for (const p of pads) {
        if (p && p.connected && p.mapping === 'standard') {
          if (p.buttons[PAD_BUTTON.BACK]?.pressed && p.buttons[PAD_BUTTON.START]?.pressed) {
            both = true
            break
          }
        }
      }
      const c = chordRef.current
      if (both) {
        if (!c.since) c.since = now
        else if (!c.fired && now - c.since >= 1500) {
          c.fired = true
          setExitConfirm(true)
        }
      } else {
        c.since = 0
        c.fired = false
      }
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [kind])

  // 弹窗内手柄操作：A=确认退出，B=取消
  useGamepad({
    enabled: exitConfirm,
    allowActions: ['confirm', 'back'],
    onAction: (action) => {
      if (action === 'confirm') onExit()
      else if (action === 'back') setExitConfirm(false)
    },
  })

  return (
    <div
      className={[
        'flex h-full flex-col bg-black',
        // 伪全屏：脱离常规布局，铺满整个视口并置顶，隐藏其它 UI
        pseudoFs ? 'fixed inset-0 z-50' : '',
      ].join(' ')}
    >
      <div className={['items-center justify-between px-3 py-2 text-white', pseudoFs ? 'hidden' : 'flex'].join(' ')}>
        <button
          onClick={onExit}
          className="flex items-center gap-1 text-sm font-bold"
        >
          <ArrowLeft size={18} /> 返回游戏大厅
        </button>
        <div className="truncate px-2 text-sm font-black">{game.name}</div>
        <div className="flex items-center gap-3">
          <button onClick={() => setRefreshKey((k) => k + 1)} title="刷新游戏">
            <RotateCw size={18} />
          </button>
          <button onClick={toggleFullscreen} title="全屏">
            <Maximize2 size={18} />
          </button>
        </div>
      </div>

      {/* 评分栏：我的评分（1-5 星，可取消） + 综合评分（静态源游戏无本地 game_id，不显示） */}
      {!game._static && (
      <div
        className={[
          'flex-wrap items-center justify-center gap-1.5 border-b border-white/10 px-3 py-1.5 text-white',
          pseudoFs ? 'hidden' : 'flex',
        ].join(' ')}
      >
        <span className="mr-1 text-xs text-white/60">我的评分</span>
        {[1, 2, 3, 4, 5].map((s) => (
          <button key={s} onClick={() => onRate(s)} title={`${s} 星`} className="text-amber-400">
            <Star size={16} className={s <= myScore ? 'fill-amber-400' : ''} />
          </button>
        ))}
        {myScore > 0 && (
          <button onClick={onUnrate} className="ml-1 text-xs text-white/60 hover:text-white">
            取消
          </button>
        )}
        <span className="ml-2 text-xs text-white/60">
          综合 {agg.rating_count > 0 ? Number(agg.avg_rating).toFixed(1) : '暂无'}（{agg.rating_count}{' '}
          人评分）
        </span>
      </div>
      )}

      {/*
        游玩区域：
        - ROM / Flash 走模拟器页面，本身已按原始比例 letterbox，直接铺满容器即可。
        - H5 游戏多为竖屏设计，在 PC 宽屏下会被横向拉伸。
          这里统一为 H5 套一个「手机模型」外框：默认 9:16 竖屏并居中，两侧留黑边；
          可用右下角悬浮球切换为 16:9 横屏（按游戏记忆，默认取后台 is_landscape）。
      */}
      <div className="relative flex-1">
        {loading && (
          <div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 text-white">
            <Spinner />
            <span className="text-sm">正在准备游戏资源...</span>
          </div>
        )}
        {src &&
          (kind === 'h5' ? (
            <div ref={wrapRef} className="absolute inset-0 flex items-center justify-center overflow-hidden bg-black">
              <iframe
                key={`${src}-${refreshKey}`}
                ref={frameRef}
                src={src}
                title={game.name}
                className={[
                  'h-auto w-auto max-h-full max-w-full rounded-xl border-0 bg-black shadow-2xl',
                  orient === 'landscape' ? 'aspect-video' : 'aspect-[9/16]',
                ].join(' ')}
                allow="fullscreen; autoplay; gamepad; accelerometer; gyroscope"
                onLoad={() => {
                  setLoading(false)
                  pushSpeed(speedRef.current)
                }}
              />
            </div>
          ) : (
            <iframe
              key={`${src}-${refreshKey}`}
              ref={frameRef}
              src={src}
              title={game.name}
              className="h-full w-full border-0 bg-black"
              allow="fullscreen; autoplay; gamepad; accelerometer; gyroscope"
              onLoad={() => setLoading(false)}
            />
          ))}
      </div>

      {/* 悬浮球：拖拽移动，点击展开快捷操作；伪全屏时隐藏（真全屏由浏览器自动隐藏） */}
      {!pseudoFs && (
      <FloatingBall
        items={[
          {
            key: 'orient',
            label: orient === 'portrait' ? '切换为横屏' : '切换为竖屏',
            icon: <RotateCcw size={14} />,
            onClick: switchOrient,
          },
          {
            key: 'fullscreen',
            // 竖屏走伪全屏（开/关），横屏走系统全屏（开/退）
            label:
              orient === 'landscape'
                ? isFs
                  ? '退出全屏'
                  : '全屏'
                : pseudoFs
                  ? '退出全屏'
                  : '全屏',
            icon: <Maximize2 size={14} />,
            onClick: toggleFullscreen,
          },
          {
            key: 'refresh',
            label: '刷新游戏',
            icon: <RotateCw size={14} />,
            onClick: () => setRefreshKey((k) => k + 1),
          },
          { key: 'exit', label: '返回游戏大厅', icon: <Home size={14} />, onClick: onExit },
        ]}
      >
        {kind === 'h5' && (
          <div className="rounded-xl border border-white/15 bg-black/75 px-3 py-2 backdrop-blur">
            <div className="mb-1.5 text-[11px] font-bold text-white/70">倍速（H5 适用）</div>
            <div className="flex flex-wrap gap-1">
              {SPEED_PRESETS.map((s) => (
                <button
                  key={s}
                  onClick={() => applySpeed(s)}
                  className={[
                    'rounded-lg px-2 py-1 text-[11px] font-bold transition',
                    speed === s
                      ? 'bg-theme-accent text-white'
                      : 'bg-white/10 text-white/80 hover:bg-white/20',
                  ].join(' ')}
                >
                  {s}x
                </button>
              ))}
            </div>
            {speed !== 1 && (
              <div className="mt-1 text-[10px] text-white/50">部分游戏引擎可能不生效</div>
            )}
          </div>
        )}
      </FloatingBall>
      )}

      {/* 伪全屏：右上角提供一个退出全屏按钮（悬浮球与顶栏此时均隐藏） */}
      {pseudoFs && (
        <button
          onClick={() => setPseudoFs(false)}
          title="退出全屏"
          className="fixed right-3 top-3 z-[60] flex h-9 w-9 items-center justify-center rounded-full border border-white/20 bg-black/50 text-white backdrop-blur"
        >
          <Minimize2 size={16} />
        </button>
      )}

      {exitConfirm && (
        <div className="absolute inset-0 z-30 flex items-center justify-center bg-black/70">
          <div className="w-72 rounded-2xl border border-theme-border bg-theme-panel p-5 text-center">
            <div className="mb-3 text-sm font-black text-theme-text">退出游戏？</div>
            <div className="mb-4 text-xs text-theme-sub">
              手柄按 A 确认退出，按 B 取消
            </div>
            <div className="flex justify-center gap-3">
              <button
                onClick={() => setExitConfirm(false)}
                className="rounded-xl border border-theme-border px-4 py-2 text-sm font-bold text-theme-sub"
              >
                取消
              </button>
              <button
                onClick={onExit}
                className="rounded-xl bg-theme-accent px-4 py-2 text-sm font-bold text-white"
              >
                退出
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
