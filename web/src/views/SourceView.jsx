import React, { useState } from 'react'
import { Plus, Trash2, Check, Globe, HardDrive, RefreshCw, Boxes } from 'lucide-react'
import Button from '../components/ui/Button'
import Input from '../components/ui/Input'
import { useToast } from '../contexts/ToastContext'
import {
  getAllSources,
  getCurrentSourceId,
  setCurrentSourceId,
  upsertRemoteSource,
  removeRemoteSource,
  discoverSource,
  normalizeSourceDescriptor,
  isStaticSource,
  LOCAL_SOURCE_ID,
} from '../lib/source'

// 源管理面板：展示全部源（本地源 + 远程源），支持切换 / 添加 / 删除
// 支持两类源（见 lib/source.js）：
//   - 动态源（dynamic）：走 API，需后端提供 manifest/search/detail
//   - 静态源（static）：走静态托管（GitHub Pages），清单一次性拉取 + 前端本地搜索
// 添加方式：
//   1) 填地址 → 自动发现（先试动态源 / 再试静态源）
//   2) 直接粘贴源描述 JSON
export default function SourcePanel() {
  const toast = useToast()
  const [sources, setSources] = useState(getAllSources())
  const [currentId, setCurrentId] = useState(getCurrentSourceId())
  const [mode, setMode] = useState('discover') // discover | paste
  const [input, setInput] = useState('')
  const [json, setJson] = useState('')
  const [busy, setBusy] = useState(false)

  // 重新从 localStorage 载入源列表
  const refresh = () => {
    setSources(getAllSources())
    setCurrentId(getCurrentSourceId())
  }

  const handleSwitch = (id) => {
    setCurrentSourceId(id)
    setCurrentId(id)
    toast.success('已切换源（下次拉取生效）')
  }

  const handleRemove = (id) => {
    removeRemoteSource(id)
    refresh()
    toast.success('已删除源')
  }

  // 添加源：自动发现
  const handleDiscover = async () => {
    setBusy(true)
    try {
      const { ok, source, error } = await discoverSource(input)
      if (!ok) {
        toast.error(error)
        return
      }
      upsertRemoteSource(source)
      refresh()
      setInput('')
      toast.success(`已添加源：${source.name}`)
    } finally {
      setBusy(false)
    }
  }

  // 添加源：粘贴 source.json
  const handlePaste = () => {
    let desc
    try {
      desc = JSON.parse(json)
    } catch {
      toast.error('不是合法 JSON')
      return
    }
    const { ok, source, error } = normalizeSourceDescriptor(desc)
    if (!ok) {
      toast.error(error)
      return
    }
    upsertRemoteSource(source)
    refresh()
    setJson('')
    toast.success(`已添加源：${source.name}`)
  }

  return (
    <div className="space-y-4">
      {/* 已有源列表 */}
      <div className="space-y-2">
        {sources.map((s) => {
          const isLocal = s.id === LOCAL_SOURCE_ID
          const isStatic = isStaticSource(s)
          const active = s.id === currentId
          return (
            <div
              key={s.id}
              className="flex items-center gap-3 rounded-2xl border border-theme-border bg-theme-panel p-3"
            >
              {isLocal ? (
                <HardDrive size={20} className="text-theme-accent" />
              ) : isStatic ? (
                <Boxes size={20} className="text-theme-accent" />
              ) : (
                <Globe size={20} className="text-theme-accent" />
              )}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-bold text-theme-text">{s.name}</span>
                  {!isLocal && (
                    <span className="shrink-0 rounded bg-theme-accent/15 px-1.5 py-0.5 text-[10px] font-bold text-theme-accent">
                      {isStatic ? '静态' : '动态'}
                    </span>
                  )}
                </div>
                <div className="truncate text-[11px] text-theme-sub">
                  {isLocal ? '本机源（内置）' : s.baseUrl}
                </div>
              </div>
              {active ? (
                <span className="flex items-center gap-1 text-xs font-bold text-theme-accent">
                  <Check size={14} /> 使用中
                </span>
              ) : (
                <button
                  onClick={() => handleSwitch(s.id)}
                  className="rounded-lg bg-theme-accent/10 px-2 py-1 text-xs font-bold text-theme-accent"
                >
                  切换
                </button>
              )}
              {!isLocal && (
                <button
                  onClick={() => handleRemove(s.id)}
                  className="text-theme-sub hover:text-red-400"
                  aria-label="删除源"
                >
                  <Trash2 size={16} />
                </button>
              )}
            </div>
          )
        })}
      </div>

      {/* 添加源 */}
      <div className="rounded-2xl border border-theme-border bg-theme-panel p-3">
        <div className="mb-2 flex items-center gap-2">
          <Plus size={16} className="text-theme-accent" />
          <span className="text-sm font-bold text-theme-text">添加源</span>
        </div>

        <div className="mb-2 flex gap-2 text-xs">
          <button
            onClick={() => setMode('discover')}
            className={`rounded-lg px-2 py-1 font-bold ${
              mode === 'discover' ? 'bg-theme-accent/15 text-theme-accent' : 'text-theme-sub'
            }`}
          >
            填域名自动发现
          </button>
          <button
            onClick={() => setMode('paste')}
            className={`rounded-lg px-2 py-1 font-bold ${
              mode === 'paste' ? 'bg-theme-accent/15 text-theme-accent' : 'text-theme-sub'
            }`}
          >
            粘贴源描述
          </button>
        </div>

        {mode === 'discover' ? (
          <div className="flex gap-2">
            <Input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="源地址（动态源域名 / 静态源 Pages 地址）"
            />
            <Button onClick={handleDiscover} disabled={busy}>
              {busy ? <RefreshCw size={16} className="animate-spin" /> : '发现'}
            </Button>
          </div>
        ) : (
          <div className="space-y-2">
            <textarea
              value={json}
              onChange={(e) => setJson(e.target.value)}
              rows={6}
              placeholder='{ "protocol": "uogame-source", "version": 1, "type": "static", ... }'
              className="w-full rounded-xl border border-theme-border bg-theme-panel px-3 py-2 font-mono text-xs text-theme-text placeholder:text-theme-sub focus:border-theme-accent focus:outline-none"
            />
            <Button onClick={handlePaste} className="w-full">
              添加
            </Button>
          </div>
        )}
      </div>

      <p className="text-[11px] leading-relaxed text-theme-sub">
        提示：「本机源」始终指向当前后端，不可删除。远程源需符合{' '}
        <code className="mx-1">uogame-source</code>
        协议：<b>动态源</b>走 API；<b>静态源</b>（如 GitHub Pages）一次性拉取清单，在前端本地搜索、在线播放。
      </p>
    </div>
  )
}
