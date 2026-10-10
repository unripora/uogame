import React, { useState } from 'react'
import { Tags, Users, Cog, Gamepad2, Upload, ShieldCheck, Joystick } from 'lucide-react'
import CategoryPanel from './admin/CategoryPanel'
import UserPanel from './admin/UserPanel'
import SystemPanel from './admin/SystemPanel'
import GameManagePanel from './admin/GameManagePanel'
import ImportPanel from './admin/ImportPanel'
import WhitelistPanel from './admin/WhitelistPanel'
import GamepadPanel from './admin/GamepadPanel'

// 管理面板容器：侧栏切换各管理面板
const PANELS = [
  { id: 'games', label: '游戏管理', icon: Gamepad2, comp: GameManagePanel },
  { id: 'import', label: '导入', icon: Upload, comp: ImportPanel },
  { id: 'categories', label: '分类配置', icon: Tags, comp: CategoryPanel },
  { id: 'whitelist', label: '白名单分类', icon: ShieldCheck, comp: WhitelistPanel },
  { id: 'users', label: '用户管理', icon: Users, comp: UserPanel },
  { id: 'system', label: '系统设置', icon: Cog, comp: SystemPanel },
  { id: 'gamepad', label: '手柄调试', icon: Joystick, comp: GamepadPanel },
]

export default function AdminView() {
  const [active, setActive] = useState('games')
  const current = PANELS.find((p) => p.id === active)
  const Comp = current ? current.comp : null

  return (
    <div className="flex h-full flex-col">
      <div className="flex gap-1 overflow-x-auto border-b border-theme-border bg-theme-panel px-2 py-2">
        {PANELS.map((p) => {
          const Icon = p.icon
          const on = active === p.id
          return (
            <button
              key={p.id}
              onClick={() => setActive(p.id)}
              className={[
                'flex shrink-0 items-center gap-1 rounded-full px-3 py-1.5 text-xs font-bold',
                on ? 'bg-theme-accent text-white' : 'text-theme-sub hover:text-theme-text',
              ].join(' ')}
            >
              <Icon size={14} /> {p.label}
            </button>
          )
        })}
      </div>
      <div className="flex-1 overflow-y-auto p-4">{Comp && <Comp />}</div>
    </div>
  )
}
