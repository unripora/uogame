import React, { useState } from 'react'
import { User as UserIcon, Save, Palette, Info, LogOut, ChevronRight, Image as ImageIcon, Repeat, Globe } from 'lucide-react'
import Button from '../components/ui/Button'
import Input from '../components/ui/Input'
import Modal from '../components/ui/Modal'
import { useAuth } from '../contexts/AuthContext'
import { useTheme } from '../contexts/ThemeContext'
import { useToast } from '../contexts/ToastContext'
import { useCategories } from '../hooks/useCategories'
import { useGames } from '../hooks/useGames'
import { listSaveSummary, exportSingleGame, exportAllGames } from '../lib/saveExport'
import { parseExportFile, buildImportPlan, importSaves } from '../lib/saveImport'
import { DEFAULT_COVERS, readCoverMap, writeCoverMap } from '../hooks/useDefaultCover'
import { putJSON, postJSON, getJSON, setToken } from '../api/client'
import { API } from '../api/endpoints'
import { LS, lsSetStr } from '../hooks/usePersistence'
import SourcePanel from './SourceView'

// 我的：资料 / 存档管理 / 外观主题 / 关于 / 退出
export default function ProfileView() {
  const { user, logout, refreshUser, switchIdentity, switchTo } = useAuth()
  const toast = useToast()
  const [panel, setPanel] = useState(null) // profile | saves | theme | about
  const [switching, setSwitching] = useState(false)

  const isManager = !!(user && user.account_type === 'manager')
  const accountCode = user ? user.uid || user.manager_code || '' : ''
  const roleLabel = isManager
    ? user.role === 'super_admin'
      ? '超级管理员'
      : '系统管理员'
    : '普通玩家'

  const entries = [
    { id: 'profile', label: '资料设置', desc: '修改昵称或密码', icon: UserIcon },
    { id: 'saves', label: '存档管理', desc: '备份与还原云端进度', icon: Save },
    { id: 'theme', label: '外观与主题', desc: '赛博霓虹、复古街机等风格', icon: Palette },
    { id: 'covers', label: '默认封面', desc: '为每个分类挑选默认封面', icon: ImageIcon },
    { id: 'sources', label: '游戏源', desc: '添加 / 切换在线游戏源', icon: Globe },
    { id: 'about', label: '关于应用', desc: '致谢与开源说明', icon: Info },
  ]

  const handleSwitch = async () => {
    if (!switchIdentity) return
    setSwitching(true)
    try {
      await switchTo(switchIdentity.account_type)
      toast.success(
        switchIdentity.account_type === 'manager' ? '已切换为管理员' : '已切换为玩家'
      )
    } catch (e) {
      toast.error(e.message || '切换失败')
    } finally {
      setSwitching(false)
    }
  }

  return (
    <div className="p-4">
      <header className="mb-4 flex items-center gap-3">
        <div className="flex h-14 w-14 items-center justify-center overflow-hidden rounded-full bg-theme-accent text-white">
          {user && user.avatar ? (
            <img src={user.avatar} alt="" className="h-full w-full object-cover" />
          ) : (
            <UserIcon size={26} />
          )}
        </div>
        <div>
          <div className="text-base font-black text-theme-text">
            {user ? user.nickname || user.username : '未登录'}
          </div>
          <div className="text-xs text-theme-sub">
            {roleLabel}
            {accountCode ? <span className="ml-2 font-mono">#{accountCode}</span> : null}
          </div>
        </div>
      </header>

      {/* 身份切换：仅对「可切换身份」的人显示——普通玩家完全不可见（保密） */}
      {switchIdentity ? (
        <button
          onClick={handleSwitch}
          disabled={switching}
          className="mb-3 flex w-full items-center gap-3 rounded-2xl border border-theme-accent/40 bg-theme-accent/10 p-3 text-left"
        >
          <Repeat size={20} className="text-theme-accent" />
          <div className="flex-1">
            <div className="text-sm font-bold text-theme-text">
              切换为{switchIdentity.account_type === 'manager' ? '管理员模式' : '玩家模式'}
            </div>
            <div className="text-[11px] text-theme-sub">
              {switchIdentity.nickname || switchIdentity.username}
              {switchIdentity.account_code ? ` · #${switchIdentity.account_code}` : ''}
              （免密切换）
            </div>
          </div>
          <ChevronRight size={18} className="text-theme-sub" />
        </button>
      ) : isManager ? (
        <div className="mb-3 flex w-full items-center gap-3 rounded-2xl border border-theme-border bg-theme-panel p-3">
          <Repeat size={20} className="text-theme-sub" />
          <div className="flex-1">
            <div className="text-sm font-bold text-theme-text">未绑定玩家账号</div>
            <div className="text-[11px] text-theme-sub">
              当前管理员账号未关联玩家身份，暂无法切换。可在管理后台「用户管理 → 人员绑定」中关联。
            </div>
          </div>
        </div>
      ) : null}

      <div className="space-y-2">
        {entries.map((e) => {
          const Icon = e.icon
          return (
            <button
              key={e.id}
              onClick={() => setPanel(e.id)}
              className="flex w-full items-center gap-3 rounded-2xl border border-theme-border bg-theme-panel p-3 text-left"
            >
              <Icon size={20} className="text-theme-accent" />
              <div className="flex-1">
                <div className="text-sm font-bold text-theme-text">{e.label}</div>
                <div className="text-[11px] text-theme-sub">{e.desc}</div>
              </div>
              <ChevronRight size={18} className="text-theme-sub" />
            </button>
          )
        })}
      </div>

      <Button
        variant="danger"
        className="mt-4 w-full"
        onClick={async () => {
          await logout()
        }}
      >
        <LogOut size={16} /> 退出登录
      </Button>

      {panel === 'profile' && (
        <ProfileModal user={user} onClose={() => setPanel(null)} onSaved={refreshUser} />
      )}
      {panel === 'theme' && <ThemeModal onClose={() => setPanel(null)} />}
      {panel === 'covers' && <CoversModal onClose={() => setPanel(null)} />}
      {panel === 'sources' && (
        <Modal open title="游戏源" onClose={() => setPanel(null)}>
          <SourcePanel />
        </Modal>
      )}
      {panel === 'saves' && <SaveModal onClose={() => setPanel(null)} />}
      {panel === 'about' && <AboutModal onClose={() => setPanel(null)} />}
    </div>
  )
}

function ProfileModal({ user, onClose, onSaved }) {
  const toast = useToast()
  const [username, setUsername] = useState((user && user.username) || '')
  const [nickname, setNickname] = useState((user && user.nickname) || '')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const save = async () => {
    setBusy(true)
    try {
      const data = await putJSON(API.userProfile, { username, nickname, password })
      // 用户名变更时后端会返回新 token，同步到本地
      if (data && data.token) {
        setToken(data.token)
        lsSetStr(LS.token, data.token)
      }
      toast.success('用户信息修改成功')
      onSaved && onSaved(data && data.user ? data.user : { ...user, nickname })
      onClose()
    } catch (e) {
      toast.error(e.message || '保存失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title="编辑个人资料"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={busy}>
            保存
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Input
          placeholder="用户名"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
        />
        <Input
          placeholder="新昵称"
          value={nickname}
          onChange={(e) => setNickname(e.target.value)}
        />
        <Input
          type="password"
          placeholder="修改密码 (留空不改)"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>
    </Modal>
  )
}

function ThemeModal({ onClose }) {
  const { theme, setTheme, themes } = useTheme()
  return (
    <Modal open title="外观风格与主题" onClose={onClose}>
      <div className="space-y-2">
        {themes.map((t) => (
          <button
            key={t.id}
            onClick={() => setTheme(t.id)}
            className={[
              'flex w-full items-center justify-between rounded-xl border p-3 text-left',
              theme === t.id
                ? 'border-theme-accent bg-theme-accent/10'
                : 'border-theme-border',
            ].join(' ')}
          >
            <div>
              <div className="text-sm font-bold text-theme-text">
                {t.name} <span className="text-theme-sub">· {t.subtitle}</span>
              </div>
              <div className="text-[11px] text-theme-sub">{t.desc}</div>
            </div>
            {theme === t.id && (
              <span className="text-xs font-black text-theme-accent">使用中</span>
            )}
          </button>
        ))}
      </div>
      <p className="mt-3 text-[11px] text-theme-sub">
        ✨ 切换后自动实时生效并持久保存。
      </p>
    </Modal>
  )
}

// 默认封面设置：为每个分类挑选一张内置封面（读者级，存本机 localStorage）
// 未配置的分类 → 无封面游戏按 id/名称稳定随机选一张
function CoversModal({ onClose }) {
  const toast = useToast()
  const { categories } = useCategories()
  const [map, setMap] = useState(() => readCoverMap())

  const choose = (cid, coverId) => {
    setMap((prev) => {
      const next = { ...prev }
      if (coverId) next[String(cid)] = coverId
      else delete next[String(cid)]
      return next
    })
  }

  const save = () => {
    writeCoverMap(map)
    toast.success('默认封面已保存')
    onClose()
  }

  return (
    <Modal
      open
      title="默认封面"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button onClick={save}>保存</Button>
        </>
      }
    >
      <p className="mb-3 text-[12px] text-theme-sub">
        为每个分类挑选一张默认封面（仅本设备生效）；未配置的分类将自动随机分配。
      </p>
      {categories.length === 0 ? (
        <div className="py-4 text-center text-[13px] text-theme-sub">暂无分类</div>
      ) : (
        <div className="max-h-80 space-y-3 overflow-y-auto pr-1">
          {categories.map((c) => {
            const current = map[String(c.id)]
            return (
              <div key={c.id} className="rounded-xl border border-theme-border p-2">
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-sm font-bold text-theme-text">{c.name}</span>
                  <button
                    onClick={() => choose(c.id, null)}
                    className={[
                      'rounded-full px-2 py-0.5 text-[11px] font-bold transition',
                      current ? 'text-theme-sub hover:text-theme-accent' : 'bg-theme-accent text-white',
                    ].join(' ')}
                  >
                    随机
                  </button>
                </div>
                <div className="flex gap-2">
                  {DEFAULT_COVERS.map((url, i) => {
                    const id = i + 1
                    const active = current === id
                    return (
                      <button
                        key={id}
                        onClick={() => choose(c.id, active ? null : id)}
                        title={`封面 ${id}`}
                        className={[
                          'h-12 w-20 overflow-hidden rounded-lg border-2 transition',
                          active ? 'border-theme-accent' : 'border-transparent hover:border-theme-border',
                        ].join(' ')}
                      >
                        <img src={url} alt={`封面 ${id}`} className="h-full w-full object-cover" />
                      </button>
                    )
                  })}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </Modal>
  )
}

function SaveModal({ onClose }) {
  const toast = useToast()
  const [backups, setBackups] = useState(null)
  const [summary, setSummary] = useState(null)
  const [busy, setBusy] = useState('')
  const [pending, setPending] = useState(null) // { payload, plan } 导入预览
  const [importing, setImporting] = useState(false)
  const fileRef = React.useRef(null)
  const { games } = useGames()

  const reloadBackups = async () => {
    try {
      const data = await getJSON(`${API.userBackups}?t=${Date.now()}`)
      setBackups(Array.isArray(data) ? data : [])
    } catch {
      setBackups([])
    }
  }

  React.useEffect(() => {
    ;(async () => {
      await reloadBackups()
      try {
        setSummary(await listSaveSummary())
      } catch {
        setSummary([])
      }
    })()
  }, [])

  const gameNameOf = (gid) => {
    const g = (games || []).find((x) => String(x.id) === String(gid))
    return (g && g.name) || `游戏 #${gid}`
  }

  const create = async () => {
    try {
      const title = prompt('输入备份标题...') || ''
      await postJSON(API.userBackups, { title, type: 'manual' })
      toast.success('备份已创建')
      await reloadBackups()
    } catch (e) {
      toast.error(e.message || '操作失败')
    }
  }

  const doExportOne = async (gid) => {
    setBusy(String(gid))
    try {
      const r = await exportSingleGame(gid, gameNameOf(gid))
      if (r.ok) toast.success('已导出该游戏存档')
      else toast.error('该游戏暂无存档可导出')
    } catch (e) {
      toast.error(e.message || '导出失败')
    } finally {
      setBusy('')
    }
  }

  const doExportAll = async () => {
    setBusy('all')
    try {
      const r = await exportAllGames(games)
      if (r.ok) toast.success('已导出全部游戏存档')
      else toast.error('本机暂无存档可导出')
    } catch (e) {
      toast.error(e.message || '导出失败')
    } finally {
      setBusy('')
    }
  }

  const refreshSummary = async () => {
    try {
      setSummary(await listSaveSummary())
    } catch {
      setSummary([])
    }
  }

  const pickImport = () => {
    if (fileRef.current) fileRef.current.click()
  }

  const onImportFile = async (e) => {
    const file = e.target.files && e.target.files[0]
    e.target.value = '' // 允许重复选择同一文件
    if (!file) return
    try {
      const parsed = parseExportFile(await file.text())
      if (!parsed.ok) {
        toast.error(parsed.error || '文件解析失败')
        return
      }
      setPending({ payload: parsed.payload, plan: buildImportPlan(parsed.payload, games) })
    } catch (err) {
      toast.error('读取文件失败')
    }
  }

  const doImport = async () => {
    if (!pending) return
    setImporting(true)
    try {
      const r = await importSaves(pending.payload, games)
      const skipped = r.rom.skipped + r.h5Local.skipped + r.h5DB.skipped
      const parts = [`ROM ${r.rom.written}`, `H5 键 ${r.h5Local.written}`, `H5 记录 ${r.h5DB.written}`]
      toast.success(`导入完成：${parts.join(' / ')}${skipped ? `，跳过 ${skipped}` : ''}`)
      setPending(null)
      await refreshSummary()
    } catch (e) {
      toast.error(e.message || '导入失败')
    } finally {
      setImporting(false)
    }
  }

  const skipReasonText = (reason) => (reason === 'no-name' ? '文件缺少游戏名（v1 格式）' : '本机无同名游戏')

  const fmtBytes = (n) => {
    if (!n) return '0 B'
    if (n >= 1048576) return `${(n / 1048576).toFixed(1)} MB`
    if (n >= 1024) return `${Math.round(n / 1024)} KB`
    return `${n} B`
  }

  const hasLocal = !!(summary && summary.length)

  return (
    <Modal
      open
      title="我的存档"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={doExportAll} disabled={busy === 'all' || !hasLocal}>
            导出所有游戏存档
          </Button>
          <Button onClick={create}>新建备份</Button>
        </>
      }
    >
      <div className="mb-4">
        <div className="mb-2 flex items-center justify-between">
          <div className="text-[13px] font-bold text-theme-text">导入存档（从文件恢复）</div>
          <Button
            variant="ghost"
            className="!px-3 !py-1 text-[12px]"
            onClick={pickImport}
            disabled={importing}
          >
            选择文件
          </Button>
        </div>
        <input
          ref={fileRef}
          type="file"
          accept=".json,application/json"
          className="hidden"
          onChange={onImportFile}
        />
        {pending ? (
          <div className="rounded-xl border border-theme-border p-3">
            {pending.plan.matched.length > 0 && (
              <>
                <div className="mb-1 text-[12px] font-bold text-theme-text">
                  将导入 {pending.plan.matched.length} 个游戏
                </div>
                <ul className="mb-2 space-y-0.5">
                  {pending.plan.matched.map((m) => (
                    <li key={m.exportId} className="text-[11px] text-theme-sub">
                      · {m.gameName || `#${m.exportId}`} → {gameNameOf(m.localId)}（ROM {m.romCount} · H5键{' '}
                      {m.h5Keys} · H5库 {m.h5DBs}）
                    </li>
                  ))}
                </ul>
              </>
            )}
            {pending.plan.skipped.length > 0 && (
              <>
                <div className="mb-1 text-[12px] font-bold text-theme-text">
                  跳过 {pending.plan.skipped.length} 个
                </div>
                <ul className="mb-2 space-y-0.5">
                  {pending.plan.skipped.map((s) => (
                    <li key={s.exportId} className="text-[11px] text-amber-500">
                      · {s.gameName || `#${s.exportId}`}：{skipReasonText(s.reason)}
                    </li>
                  ))}
                </ul>
              </>
            )}
            {pending.plan.matched.length === 0 && (
              <div className="mb-2 text-[11px] text-amber-500">
                没有可导入的游戏，请确认目标机已安装同名游戏。
              </div>
            )}
            <div className="flex justify-end gap-2">
              <Button
                variant="ghost"
                className="!px-3 !py-1 text-[12px]"
                onClick={() => setPending(null)}
                disabled={importing}
              >
                取消
              </Button>
              <Button
                className="!px-3 !py-1 text-[12px]"
                onClick={doImport}
                disabled={importing || pending.plan.matched.length === 0}
              >
                确认导入
              </Button>
            </div>
          </div>
        ) : (
          <div className="rounded-xl border border-theme-border p-3 text-center text-[12px] text-theme-sub">
            选择此前导出的存档 JSON 文件，将按「游戏名」映射到本机同名游戏
          </div>
        )}
      </div>

      <div className="mb-4">
        <div className="mb-2 text-[13px] font-bold text-theme-text">本机存档（导出到文件）</div>
        {summary === null ? (
          <div className="py-2 text-center text-[13px] text-theme-sub">读取中...</div>
        ) : summary.length === 0 ? (
          <div className="rounded-xl border border-theme-border p-3 text-center text-[13px] text-theme-sub">
            本机暂无游戏存档，游玩并保存后即可导出
          </div>
        ) : (
          <div className="max-h-56 space-y-2 overflow-y-auto">
            {summary.map((s) => {
              const meta = [
                s.romCount ? `${s.romCount} 个存档 · ${fmtBytes(s.romBytes)}` : '',
                s.h5Keys ? `H5 键 ${s.h5Keys}` : '',
                s.h5DBs ? `H5 库 ${s.h5DBs}` : '',
              ]
                .filter(Boolean)
                .join(' · ')
              return (
                <div
                  key={s.gameId}
                  className="flex items-center justify-between gap-2 rounded-xl border border-theme-border p-2"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm text-theme-text">{gameNameOf(s.gameId)}</div>
                    <div className="text-[11px] text-theme-sub">{meta}</div>
                  </div>
                  <Button
                    variant="ghost"
                    className="shrink-0 !px-3 !py-1 text-[12px]"
                    onClick={() => doExportOne(s.gameId)}
                    disabled={busy === String(s.gameId)}
                  >
                    导出
                  </Button>
                </div>
              )
            })}
          </div>
        )}
      </div>

      <div className="mb-2 text-[13px] font-bold text-theme-text">云端备份</div>
      {backups === null ? (
        <div className="py-4 text-center text-theme-sub">加载中...</div>
      ) : backups.length === 0 ? (
        <div className="py-4 text-center text-[13px] text-theme-sub">
          暂无存档备份，点击“新建备份”将当前云存档存入快照
        </div>
      ) : (
        <div className="max-h-52 space-y-2 overflow-y-auto">
          {backups.map((b) => (
            <div
              key={b.id}
              className="flex items-center justify-between rounded-xl border border-theme-border p-2 text-sm"
            >
              <span className="text-theme-text">{b.title || `备份 #${b.id}`}</span>
              <span className="text-[11px] text-theme-sub">{b.type}</span>
            </div>
          ))}
        </div>
      )}
    </Modal>
  )
}

function AboutModal({ onClose }) {
  return (
    <Modal open title="关于应用" onClose={onClose}>
      <div className="space-y-2 text-[13px] text-theme-sub">
        <p>拾游集 UOGame · 云端游戏厅</p>
        <p>复古游戏模拟器引擎集成自开源项目 EmulatorJS 与 Ruffle。</p>
        <p>开发者：落地长安</p>
        <p className="pt-2">如果觉得好用，可以请作者喝杯咖啡 ☕</p>
        <div className="flex gap-3 pt-2">
          <img src="/alipay.png" alt="支付宝" className="h-32 w-32 rounded-lg object-contain" />
          <img src="/wechat.png" alt="微信" className="h-32 w-32 rounded-lg object-contain" />
        </div>
      </div>
    </Modal>
  )
}
