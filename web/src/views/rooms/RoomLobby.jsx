import React, { useCallback, useEffect, useState } from 'react'
import { DoorOpen, Plus, RefreshCw, Users } from 'lucide-react'
import { API } from '../../api/endpoints'
import { getJSON, postJSON } from '../../api/client'
import { useAuth } from '../../contexts/AuthContext'
import { useToast } from '../../contexts/ToastContext'
import Button from '../../components/ui/Button'
import Input from '../../components/ui/Input'
import Modal from '../../components/ui/Modal'
import Empty from '../../components/ui/Empty'
import Spinner from '../../components/ui/Spinner'

// 多人联机房间——大厅：创建 / 浏览 / 加入
// 契约：GET /api/rooms（裸数组）；POST /api/rooms（{ message, room }）
export default function RoomLobby({ onEnterRoom }) {
  const { user } = useAuth()
  const toast = useToast()
  const [rooms, setRooms] = useState(null)
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [joinTarget, setJoinTarget] = useState(null) // 需密码的房间
  const [password, setPassword] = useState('')

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getJSON(`${API.rooms}?t=${Date.now()}`)
      setRooms(Array.isArray(data) ? data : [])
    } catch {
      setRooms([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    refresh()
    const timer = setInterval(refresh, 5000)
    return () => clearInterval(timer)
  }, [refresh])

  const enterRoom = (room, pwd = '') => {
    if (onEnterRoom) onEnterRoom(room, pwd)
  }

  const handleJoin = (room) => {
    if (!user) {
      toast.info('请先登录后再加入房间')
      return
    }
    if (room.has_password) {
      setPassword('')
      setJoinTarget(room)
      return
    }
    enterRoom(room, '')
  }

  const confirmJoin = () => {
    if (!joinTarget) return
    enterRoom(joinTarget, password)
    setJoinTarget(null)
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h2 className="flex items-center gap-1.5 text-base font-black text-theme-text">
          <Users size={18} /> 联机房间
        </h2>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={refresh} disabled={loading}>
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </Button>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus size={14} /> 创建房间
          </Button>
        </div>
      </div>

      {rooms === null ? (
        <div className="flex justify-center py-10">
          <Spinner />
        </div>
      ) : rooms.length === 0 ? (
        <Empty text="暂无房间，点击「创建房间」开一局吧" />
      ) : (
        <div className="space-y-2">
          {rooms.map((r) => (
            <RoomRow key={r.id} room={r} onJoin={() => handleJoin(r)} />
          ))}
        </div>
      )}

      <CreateRoomModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(room) => {
          setCreateOpen(false)
          enterRoom(room, '')
        }}
      />

      <Modal
        open={!!joinTarget}
        title="输入房间密码"
        onClose={() => setJoinTarget(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setJoinTarget(null)}>
              取消
            </Button>
            <Button onClick={confirmJoin}>加入</Button>
          </>
        }
      >
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="房间密码"
          autoFocus
        />
      </Modal>
    </div>
  )
}

// 单条房间行
function RoomRow({ room, onJoin }) {
  const full = (room.members?.length || 0) >= room.max_users
  return (
    <div className="flex items-center justify-between rounded-2xl border border-theme-border bg-theme-panel p-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2 text-sm font-bold text-theme-text">
          <span className="truncate">{room.name || `房间 ${room.id}`}</span>
          {room.has_password ? (
            <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-bold text-amber-500">
              有密码
            </span>
          ) : null}
        </div>
        <div className="mt-0.5 text-[11px] text-theme-sub">
          房号 {room.id} · {room.members?.length || 0}/{room.max_users} 人
          {room.game_id ? ` · 游戏 #${room.game_id}` : ''}
        </div>
      </div>
      <Button variant={full ? 'subtle' : 'primary'} disabled={full} onClick={onJoin}>
        <DoorOpen size={14} /> {full ? '已满' : '加入'}
      </Button>
    </div>
  )
}

// 创建房间弹窗
function CreateRoomModal({ open, onClose, onCreated }) {
  const toast = useToast()
  const [name, setName] = useState('')
  const [gameId, setGameId] = useState('')
  const [maxUsers, setMaxUsers] = useState('4')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    try {
      const res = await postJSON(API.rooms, {
        name: name.trim(),
        game_id: Number(gameId) || 0,
        max_users: Number(maxUsers) || 4,
        password: password || '',
      })
      if (res && res.room) onCreated(res.room)
    } catch (e) {
      toast.error(e.message || '创建房间失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      title="创建房间"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button onClick={submit} disabled={busy}>
            创建
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div>
          <label className="mb-1 block text-[11px] text-theme-sub">房间名称</label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="例如：一起玩魂斗罗"
            maxLength={32}
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="mb-1 block text-[11px] text-theme-sub">关联游戏 ID（可空）</label>
            <Input
              type="number"
              value={gameId}
              onChange={(e) => setGameId(e.target.value)}
              placeholder="0"
            />
          </div>
          <div>
            <label className="mb-1 block text-[11px] text-theme-sub">人数上限（最大 8）</label>
            <Input
              type="number"
              value={maxUsers}
              onChange={(e) => setMaxUsers(e.target.value)}
              placeholder="4"
            />
          </div>
        </div>
        <div>
          <label className="mb-1 block text-[11px] text-theme-sub">房间密码（可空）</label>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="留空则公开"
          />
        </div>
      </div>
    </Modal>
  )
}
