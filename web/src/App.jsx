import React, { useCallback, useEffect, useState } from 'react'
import { ThemeProvider } from './contexts/ThemeContext'
import { ToastProvider } from './contexts/ToastContext'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import Layout from './components/Layout'
import Spinner from './components/ui/Spinner'
import Modal from './components/ui/Modal'
import Button from './components/ui/Button'
import LoginView from './views/LoginView'
import HallView from './views/HallView'
import GameDetailView from './views/GameDetailView'
import PlayView from './views/PlayView'
import CommunityView from './views/CommunityView'
import ProfileView from './views/ProfileView'
import AdminView from './views/AdminView'
import RoomLobby from './views/rooms/RoomLobby'
import RoomView from './views/rooms/RoomView'
import useGamepad from './hooks/useGamepad'
import useInputModality from './hooks/useInputModality'
import { useRoute, navigate, isInternalHistory } from './hooks/useRoute'
import { useGames } from './hooks/useGames'
import { getJSON } from './api/client'
import { API } from './api/endpoints'

// 导航顺序（与 Layout 底部导航一致），供手柄 LB/RB 切换区段
const VIEW_ORDER = ['games', 'community', 'profile', 'admin']

// 应用根组件：Provider 包裹 + 视图切换（无路由库，状态驱动）
function Shell() {
  const { user, loading, isAdmin, identityPrompt, clearIdentityPrompt, switchTo } = useAuth()
  const route = useRoute() // 轻量路由：/game/:id → 详情页；其它 → 主界面
  const { games } = useGames() // 游戏列表缓存（详情页优先命中，避免重复请求）
  const [view, setView] = useState('games') // games | community | profile | admin
  const [playing, setPlaying] = useState(null) // 正在游玩的游戏
  const [detailGame, setDetailGame] = useState(null) // 详情页游戏对象（按 id 解析）
  const [inRoom, setInRoom] = useState(null) // 已加入的房间 { room, password }
  const [showRooms, setShowRooms] = useState(false) // 是否在房间大厅

  // 详情页路由：按 id 解析游戏对象。优先命中列表缓存，未命中则单独拉取 /api/games/:id
  const detailId = route.name === 'game' ? route.id : null
  useEffect(() => {
    if (!detailId) {
      setDetailGame(null)
      return
    }
    const cached = (games || []).find((g) => String(g.id) === String(detailId))
    if (cached) {
      setDetailGame(cached)
      return
    }
    let alive = true
    setDetailGame(null)
    getJSON(API.game(detailId))
      .then((g) => {
        if (alive && g) setDetailGame(g)
      })
      .catch(() => {
        /* 拉取失败：详情页显示加载中占位 */
      })
    return () => {
      alive = false
    }
  }, [detailId, games])

  // 进入 / 返回详情页：驱动浏览器路由（保留历史，支持后退/深链接）
  const openDetail = useCallback((g) => {
    if (g && g.id != null) navigate(`/game/${g.id}`)
  }, [])
  const closeDetail = useCallback(() => {
    // 从 SPA 内部进入详情页 → 浏览器后退（与原生行为一致）
    // 深链接/直接打开详情页 → 无内部历史可退，直接替换回首页
    if (isInternalHistory()) window.history.back()
    else navigate('/', { replace: true })
  }, [])

  // 输入模态：记录鼠标/触摸/键盘/手柄，写入 <html data-input>
  useInputModality()

  // 手柄导航：面键映射到视图切换（游戏中静音，交由 PlayView 处理）
  const onPadAction = useCallback(
    (action) => {
      if (playing) return // 游玩中不响应全局导航
      const order = isAdmin ? VIEW_ORDER : VIEW_ORDER.filter((v) => v !== 'admin')
      if (action === 'prev-section' || action === 'next-section') {
        if (detailId) return // 详情页内不切换区段
        const idx = Math.max(0, order.indexOf(view))
        const delta = action === 'next-section' ? 1 : -1
        const next = (idx + delta + order.length) % order.length
        setView(order[next])
      } else if (action === 'back') {
        // B 键：详情页 → 回大厅；其它非首页 → 回游戏大厅
        if (detailId) closeDetail()
        else if (view !== 'games') setView('games')
      }
    },
    [playing, view, isAdmin, detailId, closeDetail]
  )

  useGamepad({ enabled: !playing && !!user, onAction: onPadAction })

  // 登录后识别为管理员：选择进入身份
  //   管理员 → 直接进后台；玩家 → 切换到绑定玩家（若有），否则以管理员身份浏览大厅
  const chooseEntry = useCallback(
    async (type) => {
      clearIdentityPrompt()
      if (type === 'admin') {
        setView('admin')
        return
      }
      try {
        await switchTo('player')
      } catch {
        /* 未绑定玩家账号：忽略，仍以管理员身份浏览 */
      }
      setView('games')
    },
    [clearIdentityPrompt, switchTo]
  )

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    )
  }

  // 未登录 → 登录页
  if (!user) return <LoginView />

  // 游玩中 → 全屏游玩视图
  if (playing) {
    return <PlayView game={playing} onExit={() => setPlaying(null)} />
  }

  // 详情页路由（/game/:id）→ 独立页面：脱离 Layout，无底部导航栏，自带滚动容器
  if (detailId) {
    return (
      <div key={detailId} className="h-full overflow-y-auto bg-theme-bg">
        {detailGame ? (
          <GameDetailView
            game={detailGame}
            onBack={closeDetail}
            onPlay={(g) => setPlaying(g)}
          />
        ) : (
          <div className="flex h-full items-center justify-center">
            <Spinner />
          </div>
        )}
      </div>
    )
  }

  // 已在房间内 → 房间页面
  if (inRoom) {
    return (
      <Layout view="community" onNavigate={() => {}}>
        <RoomView
          roomId={inRoom.room.id}
          password={inRoom.password}
          onLeave={() => setInRoom(null)}
          onPlay={() => setInRoom((r) => ({ ...r, playRequested: true }))}
        />
      </Layout>
    )
  }

  // 房间大厅
  if (showRooms) {
    return (
      <Layout view="community" onNavigate={() => setShowRooms(false)}>
        <div className="p-4">
          <RoomLobby
            onEnterRoom={(room, password) => {
              setShowRooms(false)
              setInRoom({ room, password })
            }}
          />
        </div>
      </Layout>
    )
  }

  let content
  if (view === 'community') {
    content = <CommunityView onOpenRooms={() => setShowRooms(true)} />
  } else if (view === 'profile') content = <ProfileView />
  else if (view === 'admin') content = <AdminView />
  else
    content = (
      <HallView
        onOpen={openDetail}
        onPlay={(g) => setPlaying(g)}
        onOpenAppearance={() => setView('profile')}
      />
    )

  return (
    <>
      <Layout view={view} onNavigate={setView}>
        {content}
      </Layout>

      <Modal
        open={!!identityPrompt}
        title="选择进入身份"
        onClose={clearIdentityPrompt}
      >
        <p className="mb-3 text-theme-sub">
          检测到当前账号具有管理员权限，请选择进入方式。
        </p>
        <div className="flex flex-col gap-2">
          <Button onClick={() => chooseEntry('admin')}>
            管理员（进入管理后台）
          </Button>
          <Button variant="ghost" onClick={() => chooseEntry('player')}>
            玩家（进入游戏大厅）
          </Button>
        </div>
      </Modal>
    </>
  )
}

export default function App() {
  return (
    <ThemeProvider>
      <ToastProvider>
        <AuthProvider>
          <Shell />
        </AuthProvider>
      </ToastProvider>
    </ThemeProvider>
  )
}
