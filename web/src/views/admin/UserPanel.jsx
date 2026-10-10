import React, { useEffect, useState } from 'react'
import { Trash2, MicOff, Mic, Shield, ShieldCheck, User as UserIcon, Plus, Link2, Unlink } from 'lucide-react'
import Input from '../../components/ui/Input'
import Button from '../../components/ui/Button'
import Spinner from '../../components/ui/Spinner'
import { API } from '../../api/endpoints'
import { getJSON, putJSON, postJSON, del } from '../../api/client'
import { useToast } from '../../contexts/ToastContext'

// 权限点定义（与后端 models 保持一致）
const PERMISSIONS = [
  { value: 'game.manage', label: '游戏管理' },
  { value: 'category.manage', label: '分类管理' },
  { value: 'user.manage', label: '用户管理' },
  { value: 'setting.manage', label: '系统设置' },
  { value: 'chat.manage', label: '聊天管理' },
  { value: 'leaderboard.manage', label: '榜单管理' },
]

const ALL_PERMS = PERMISSIONS.map((p) => p.value)

// 解析后端返回的权限点（可能是 JSON 字符串或数组）
function parsePerms(raw) {
  if (Array.isArray(raw)) return raw
  if (typeof raw !== 'string' || raw.trim() === '') return []
  try {
    const v = JSON.parse(raw)
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

// 用户管理面板：玩家列表 / 管理员列表 / 人员绑定
export default function UserPanel() {
  const [tab, setTab] = useState('players') // players | managers | binds
  return (
    <div className="space-y-4">
      <div className="flex gap-2">
        {[
          { id: 'players', label: '玩家' },
          { id: 'managers', label: '管理员' },
          { id: 'binds', label: '人员绑定' },
        ].map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={[
              'rounded-full px-4 py-1.5 text-sm font-bold',
              tab === t.id ? 'bg-theme-accent text-white' : 'bg-theme-panel text-theme-sub',
            ].join(' ')}
          >
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'players' && <PlayerList />}
      {tab === 'managers' && <ManagerList />}
      {tab === 'binds' && <BindList />}
    </div>
  )
}

// ================= 玩家列表 =================
function PlayerList() {
  const toast = useToast()
  const [users, setUsers] = useState(null)
  const [boundUIDs, setBoundUIDs] = useState([])
  const [promoting, setPromoting] = useState(null)

  const load = async () => {
    try {
      const [ps, bs] = await Promise.all([
        getJSON(`${API.adminUsers}?_t=${Date.now()}`),
        getJSON(`${API.adminBinds}?_t=${Date.now()}`),
      ])
      setUsers(Array.isArray(ps) ? ps : [])
      setBoundUIDs((Array.isArray(bs) ? bs : []).map((b) => b.player_uid))
    } catch (e) {
      toast.error(e.message || '加载失败')
      setUsers([])
      setBoundUIDs([])
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const toggleMute = async (u) => {
    try {
      // 玩家禁言在管理侧暂无独立接口，此处沿用资料接口语义：仅演示切换
      toast.info('请在玩家设置侧调整禁言状态')
    } catch (e) {
      toast.error(e.message || '操作失败')
    }
  }

  if (users === null) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <h2 className="text-base font-black text-theme-text">玩家清单</h2>
      {users.length === 0 ? (
        <div className="py-6 text-center text-[13px] text-theme-sub">暂无玩家</div>
      ) : (
        users.map((u) => {
          const bound = boundUIDs.includes(u.uid)
          return (
            <div
              key={u.id}
              className="flex items-center justify-between rounded-xl border border-theme-border bg-theme-panel p-3"
            >
              <div className="flex items-center">
                <span className="mr-2 rounded bg-theme-border px-1.5 py-0.5 font-mono text-[10px] text-theme-sub">
                  #{u.uid || String(u.id).padStart(7, '0')}
                </span>
                <span className="text-sm font-bold text-theme-text">{u.username}</span>
                {u.nickname && (
                  <span className="ml-2 text-[11px] text-theme-sub">{u.nickname}</span>
                )}
                {u.is_muted ? (
                  <span className="ml-2 inline-flex items-center gap-1 rounded-full bg-rose-500/15 px-2 py-0.5 text-[10px] font-bold text-rose-500">
                    <MicOff size={11} /> 已禁言
                  </span>
                ) : null}
              </div>
              <div className="flex items-center gap-3">
                {bound ? (
                  <span
                    className="inline-flex items-center gap-1 rounded-full bg-theme-accent/15 px-2 py-0.5 text-[10px] font-bold text-theme-accent"
                    title="已提升为管理员并绑定"
                  >
                    <Shield size={11} /> 已是管理员
                  </span>
                ) : (
                  <button
                    className="text-theme-sub hover:text-theme-accent"
                    onClick={() => setPromoting(u)}
                    title="提权为管理员（自动绑定）"
                  >
                    <Shield size={16} />
                  </button>
                )}
                <button
                  className="text-theme-sub hover:text-theme-accent"
                  onClick={() => toggleMute(u)}
                  title={u.is_muted ? '解除禁言' : '禁言'}
                >
                  {u.is_muted ? <Mic size={16} /> : <MicOff size={16} />}
                </button>
              </div>
            </div>
          )
        })
      )}

      {promoting && (
        <PromoteModal
          player={promoting}
          onClose={() => setPromoting(null)}
          onDone={() => {
            setPromoting(null)
            load()
          }}
        />
      )}
    </div>
  )
}

// 提权弹窗：将已有玩家升级为管理员，提交后自动建立人员绑定
function PromoteModal({ player, onClose, onDone }) {
  const toast = useToast()
  const [username, setUsername] = useState(player.username || '')
  const [password, setPassword] = useState(
    () => `Lg${Math.random().toString(36).slice(2, 10)}`
  )
  const [nickname, setNickname] = useState(player.nickname || '')
  const [perms, setPerms] = useState([])
  const [busy, setBusy] = useState(false)

  const togglePerm = (v) =>
    setPerms((prev) => (prev.includes(v) ? prev.filter((x) => x !== v) : [...prev, v]))

  const submit = async () => {
    if (!username.trim() || !password) {
      toast.error('请填写用户名与密码')
      return
    }
    setBusy(true)
    try {
      await postJSON(API.adminUserPromote(player.id), {
        username: username.trim(),
        password,
        nickname,
        permissions: perms,
      })
      toast.success('已提权为管理员并自动绑定')
      onDone()
    } catch (e) {
      toast.error(e.message || '提权失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-sm rounded-2xl border border-theme-border bg-theme-panel p-5">
        <h3 className="mb-1 text-base font-black text-theme-text">提权为管理员</h3>
        <p className="mb-3 text-[11px] text-theme-sub">
          玩家 {player.nickname || player.username}（#{player.uid || player.id}）将升级为管理员，
          并自动与当前玩家账号绑定，登录后可在「我的」免密切换身份。
        </p>
        <div className="space-y-2">
          <Input
            placeholder="管理员用户名"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
          <Input
            type="password"
            placeholder="管理员密码"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <Input
            placeholder="昵称（可选）"
            value={nickname}
            onChange={(e) => setNickname(e.target.value)}
          />
          <div>
            <div className="mb-1 text-[11px] font-bold text-theme-sub">
              权限点（不勾选则无任何管理权限）
            </div>
            <div className="flex flex-wrap gap-2">
              {PERMISSIONS.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  onClick={() => togglePerm(p.value)}
                  className={[
                    'rounded-full px-3 py-1 text-[11px] font-bold transition-colors',
                    perms.includes(p.value)
                      ? 'bg-theme-accent text-white'
                      : 'bg-theme-border text-theme-sub hover:text-theme-text',
                  ].join(' ')}
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={busy}>
            提权
          </Button>
        </div>
      </div>
    </div>
  )
}

// ================= 管理员列表 =================
function ManagerList() {
  const toast = useToast()
  const [managers, setManagers] = useState(null)
  const [creating, setCreating] = useState(false)

  const load = async () => {
    try {
      const data = await getJSON(`${API.adminManagers}?_t=${Date.now()}`)
      setManagers(Array.isArray(data) ? data : [])
    } catch (e) {
      toast.error(e.message || '加载失败')
      setManagers([])
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const saveManager = async (m, patch) => {
    try {
      await putJSON(API.adminManager(m.id), patch)
      toast.success('管理员信息修改成功')
      load()
      return true
    } catch (e) {
      toast.error(e.message || '操作失败')
      return false
    }
  }

  const remove = async (m) => {
    if (!confirm('删除该管理员账号？（其人员绑定也会解除，玩家账号不受影响）')) return
    try {
      await del(API.adminManager(m.id))
      toast.success('管理员已删除')
      load()
    } catch (e) {
      toast.error(e.message || '操作失败')
    }
  }

  if (managers === null) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-black text-theme-text">管理员清单</h2>
        <Button onClick={() => setCreating(true)}>
          <Plus size={14} /> 新建管理员
        </Button>
      </div>
      <div className="space-y-2">
        {managers.map((m) => (
          <ManagerRow key={m.id} manager={m} onSave={saveManager} onDelete={remove} />
        ))}
      </div>
      {creating && (
        <CreateManagerModal
          onClose={() => setCreating(false)}
          onCreated={() => {
            setCreating(false)
            load()
          }}
        />
      )}
    </div>
  )
}

function RoleBadge({ role }) {
  if (role === 'super_admin') {
    return (
      <span className="ml-2 inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-bold text-amber-500">
        <ShieldCheck size={11} /> 超级管理员
      </span>
    )
  }
  return (
    <span className="ml-2 inline-flex items-center gap-1 rounded-full bg-theme-accent/15 px-2 py-0.5 text-[10px] font-bold text-theme-accent">
      <Shield size={11} /> 管理员
    </span>
  )
}

function ManagerRow({ manager, onSave, onDelete }) {
  const [nickname, setNickname] = useState(manager.nickname || '')
  const [password, setPassword] = useState('')
  const [perms, setPerms] = useState(parsePerms(manager.permissions))
  const [allowRank, setAllowRank] = useState(manager.allow_rank === 1)
  const [enabled, setEnabled] = useState(manager.is_enabled !== 0)

  const isSuper = manager.role === 'super_admin'
  const effectivePerms = isSuper ? ALL_PERMS : perms

  const togglePerm = (value) => {
    if (isSuper) return
    setPerms((prev) =>
      prev.includes(value) ? prev.filter((x) => x !== value) : [...prev, value]
    )
  }

  const submit = async () => {
    const patch = {
      nickname,
      is_enabled: enabled ? 1 : 0,
      allow_rank: allowRank ? 1 : 0,
    }
    if (password) patch.password = password
    if (!isSuper) patch.permissions = perms
    const ok = await onSave(manager, patch)
    if (ok) setPassword('')
  }

  return (
    <div className="rounded-xl border border-theme-border bg-theme-panel p-3">
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center">
          <span className="mr-2 rounded bg-theme-border px-1.5 py-0.5 font-mono text-[10px] text-theme-sub">
            {manager.manager_code}
          </span>
          <span className="text-sm font-bold text-theme-text">{manager.username}</span>
          <RoleBadge role={manager.role} />
          {!enabled && (
            <span className="ml-2 rounded-full bg-rose-500/15 px-2 py-0.5 text-[10px] font-bold text-rose-500">
              已禁用
            </span>
          )}
        </div>
        {!isSuper && (
          <button
            className="text-theme-sub hover:text-rose-500"
            onClick={() => onDelete(manager)}
            title="删除管理员"
          >
            <Trash2 size={16} />
          </button>
        )}
      </div>

      <div className="flex flex-wrap gap-2">
        <Input
          className="flex-1"
          placeholder={manager.nickname || '昵称'}
          value={nickname}
          onChange={(e) => setNickname(e.target.value)}
        />
        <Input
          className="flex-1"
          type="password"
          placeholder="重置密码 (留空则不修改)"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Button onClick={submit}>保存</Button>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-4 text-[11px] text-theme-sub">
        <label className={`flex items-center gap-1 ${isSuper ? 'opacity-50' : ''}`}>
          <input
            type="checkbox"
            checked={enabled}
            disabled={isSuper}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          启用账号
        </label>
        <label className="flex items-center gap-1">
          <input
            type="checkbox"
            checked={allowRank}
            onChange={(e) => setAllowRank(e.target.checked)}
          />
          游玩计入榜单
        </label>
        {isSuper && <span className="text-amber-500">内置超管不可降级/禁用</span>}
      </div>

      {!isSuper && (
        <div className="mt-3">
          <div className="mb-1 text-[11px] font-bold text-theme-sub">
            权限点（不勾选则无任何管理权限）
          </div>
          <div className="flex flex-wrap gap-2">
            {PERMISSIONS.map((p) => {
              const on = effectivePerms.includes(p.value)
              return (
                <button
                  key={p.value}
                  type="button"
                  onClick={() => togglePerm(p.value)}
                  className={[
                    'rounded-full px-3 py-1 text-[11px] font-bold transition-colors',
                    on
                      ? 'bg-theme-accent text-white'
                      : 'bg-theme-border text-theme-sub hover:text-theme-text',
                  ].join(' ')}
                >
                  {p.label}
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

function CreateManagerModal({ onClose, onCreated }) {
  const toast = useToast()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [nickname, setNickname] = useState('')
  const [perms, setPerms] = useState([])
  const [busy, setBusy] = useState(false)

  const togglePerm = (v) =>
    setPerms((prev) => (prev.includes(v) ? prev.filter((x) => x !== v) : [...prev, v]))

  const submit = async () => {
    if (!username.trim() || !password) {
      toast.error('请填写用户名与密码')
      return
    }
    setBusy(true)
    try {
      await postJSON(API.adminManagers, {
        username: username.trim(),
        password,
        nickname,
        role: 'admin',
        permissions: perms,
      })
      toast.success('管理员创建成功')
      onCreated()
    } catch (e) {
      toast.error(e.message || '创建失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-sm rounded-2xl border border-theme-border bg-theme-panel p-5">
        <h3 className="mb-3 text-base font-black text-theme-text">新建管理员</h3>
        <div className="space-y-2">
          <Input
            placeholder="用户名"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
          <Input
            type="password"
            placeholder="密码"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <Input
            placeholder="昵称（可选）"
            value={nickname}
            onChange={(e) => setNickname(e.target.value)}
          />
          <div>
            <div className="mb-1 text-[11px] font-bold text-theme-sub">权限点</div>
            <div className="flex flex-wrap gap-2">
              {PERMISSIONS.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  onClick={() => togglePerm(p.value)}
                  className={[
                    'rounded-full px-3 py-1 text-[11px] font-bold transition-colors',
                    perms.includes(p.value)
                      ? 'bg-theme-accent text-white'
                      : 'bg-theme-border text-theme-sub hover:text-theme-text',
                  ].join(' ')}
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={busy}>
            创建
          </Button>
        </div>
      </div>
    </div>
  )
}

// ================= 人员绑定 =================
function BindList() {
  const toast = useToast()
  const [binds, setBinds] = useState(null)
  const [players, setPlayers] = useState([])
  const [managers, setManagers] = useState([])
  const [playerUID, setPlayerUID] = useState('')
  const [managerCode, setManagerCode] = useState('')

  const load = async () => {
    try {
      const [b, ps, ms] = await Promise.all([
        getJSON(`${API.adminBinds}?_t=${Date.now()}`),
        getJSON(API.adminUsers),
        getJSON(API.adminManagers),
      ])
      setBinds(Array.isArray(b) ? b : [])
      setPlayers(Array.isArray(ps) ? ps : [])
      setManagers(Array.isArray(ms) ? ms : [])
    } catch (e) {
      toast.error(e.message || '加载失败')
      setBinds([])
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const create = async () => {
    if (!playerUID || !managerCode) {
      toast.error('请选择玩家与管理员')
      return
    }
    try {
      await postJSON(API.adminBinds, { player_uid: playerUID, manager_code: managerCode })
      toast.success('绑定成功')
      setPlayerUID('')
      setManagerCode('')
      load()
    } catch (e) {
      toast.error(e.message || '绑定失败')
    }
  }

  const remove = async (b) => {
    if (!confirm('解除该绑定？（玩家账号不受影响）')) return
    try {
      await del(API.adminBind(b.id))
      toast.success('已解绑')
      load()
    } catch (e) {
      toast.error(e.message || '解绑失败')
    }
  }

  if (binds === null) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <h2 className="text-base font-black text-theme-text">人员绑定</h2>
      <p className="text-[11px] text-theme-sub">
        将同一人的玩家账号与管理员账号关联，登录后可在「我的」一键免密切换身份。
      </p>

      <div className="flex flex-wrap items-center gap-2 rounded-xl border border-theme-border bg-theme-panel p-3">
        <select
          className="rounded-lg border border-theme-border bg-theme-bg px-2 py-1 text-xs text-theme-text"
          value={playerUID}
          onChange={(e) => setPlayerUID(e.target.value)}
        >
          <option value="">选择玩家</option>
          {players.map((p) => (
            <option key={p.id} value={p.uid || ''}>
              {(p.uid || p.id) + ' · ' + (p.nickname || p.username)}
            </option>
          ))}
        </select>
        <Link2 size={14} className="text-theme-sub" />
        <select
          className="rounded-lg border border-theme-border bg-theme-bg px-2 py-1 text-xs text-theme-text"
          value={managerCode}
          onChange={(e) => setManagerCode(e.target.value)}
        >
          <option value="">选择管理员</option>
          {managers.map((m) => (
            <option key={m.id} value={m.manager_code}>
              {m.manager_code + ' · ' + (m.nickname || m.username)}
            </option>
          ))}
        </select>
        <Button onClick={create}>
          <Plus size={14} /> 绑定
        </Button>
      </div>

      {binds.length === 0 ? (
        <div className="py-6 text-center text-[13px] text-theme-sub">暂无绑定</div>
      ) : (
        <div className="space-y-2">
          {binds.map((b) => (
            <div
              key={b.id}
              className="flex items-center justify-between rounded-xl border border-theme-border bg-theme-panel p-3 text-sm"
            >
              <span className="text-theme-text">
                <span className="font-mono">{b.player_uid}</span>
                <span className="mx-2 text-theme-sub">↔</span>
                <span className="font-mono">{b.manager_code}</span>
              </span>
              <button
                className="text-theme-sub hover:text-rose-500"
                onClick={() => remove(b)}
                title="解绑"
              >
                <Unlink size={16} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
