import React, { useRef, useState } from 'react'
import { Upload, ShieldCheck, RotateCcw, X } from 'lucide-react'
import Button from '../../components/ui/Button'
import Input from '../../components/ui/Input'
import Select from '../../components/ui/Select'
import Spinner from '../../components/ui/Spinner'
import Empty from '../../components/ui/Empty'
import DuplicateDialog from '../../components/DuplicateDialog'
import { useCategories } from '../../hooks/useCategories'
import { useToast } from '../../contexts/ToastContext'
import { API } from '../../api/endpoints'
import { postJSON, uploadWithProgress } from '../../api/client'
import { GAME_TYPES, gameTypeByExt, SOURCE_ACCEPT } from '../../lib/gameType'

// 导入处理面板
// 功能：
//   1) 游戏包 (.zip) 批量导入
//   2) 源文件导入（ROM / SWF / HTML 单文件，类型按扩展名自动识别，可手改）
//   3) 白名单识别按钮：点击按白名单自动分类；再次点击撤回分类
export default function ImportPanel() {
  const toast = useToast()
  const { categories, reload: reloadCats } = useCategories()
  const [mode, setMode] = useState('zip') // zip | source
  const [queue, setQueue] = useState([]) // 待上传项
  const [busy, setBusy] = useState(false)
  const [classified, setClassified] = useState(false) // 白名单识别状态
  const [result, setResult] = useState(null) // 最近一次导入结果 { ok, skipped, overwritten, details }
  const [progress, setProgress] = useState(null) // 上传进度 { index, total, percent, name }
  const [dupDialog, setDupDialog] = useState(null) // 重复确认弹窗 { items:[{idx,name,existingId,existingName,reason}] }
  const [dupBusy, setDupBusy] = useState(false) // 弹窗内二次上传状态
  const zipRef = useRef(null)
  const romRef = useRef(null)

  const onPickZip = (files) => {
    const next = Array.from(files).map((f) => ({
      name: f.name,
      file: f,
      defaultName: f.name.replace(/\.zip$/i, ''),
      game_type: 'H5',
      category_id: 0,
    }))
    setQueue((q) => [...q, ...next])
    setClassified(false)
  }

  const onPickSource = (files) => {
    const next = Array.from(files).map((f) => ({
      name: f.name,
      file: f,
      defaultName: f.name.replace(/\.[^.]+$/, ''),
      // 类型按扩展名自动识别，识别不到按 H5 兜底（用户可手动改）
      game_type: gameTypeByExt(f.name) || 'H5',
      category_id: 0,
    }))
    setQueue((q) => [...q, ...next])
    setClassified(false)
  }

  const updateItem = (idx, patch) => {
    setQueue((q) => q.map((it, i) => (i === idx ? { ...it, ...patch } : it)))
  }

  const removeItem = (idx) => {
    setQueue((q) => q.filter((_, i) => i !== idx))
  }

  // 构建单个文件的 FormData（onDuplicate: 'skip' | 'overwrite'）
  const buildFormData = (item, onDuplicate) => {
    const fd = new FormData()
    fd.append('file', item.file)
    fd.append('name', item.defaultName || item.name)
    fd.append('game_type', item.game_type)
    fd.append('category_id', String(item.category_id || 0))
    if (onDuplicate === 'overwrite') {
      // 重复时覆盖（游戏包 / 源文件均支持）
      fd.append('on_duplicate', 'overwrite')
    }
    return fd
  }

  // 上传单个文件，返回 { ok, data } 或抛出错误
  const uploadOne = async (item, onDuplicate, onProgress) => {
    const url = mode === 'zip' ? API.gamesImport : API.gamesSource
    const res = await uploadWithProgress(url, buildFormData(item, onDuplicate), {
      onProgress: (_l, _t, percent) => onProgress && onProgress(percent),
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || '上传失败')
    return data
  }

  // 上传（逐个，使用 multipart）
  // 流程：逐个以 skip 上传 → 命中重复的收集到 dupItems → 有重复则弹窗确认
  //       → 勾选项二次上传 overwrite，未勾选跳过；无重复则直接出结果
  const upload = async () => {
    if (!queue.length) {
      toast.error(mode === 'zip' ? '没有符合上传条件的游戏包' : '没有可上传的源文件')
      return
    }
    setBusy(true)
    setProgress({ index: 0, total: queue.length, percent: 0, name: queue[0].name })
    let ok = 0
    let skipped = 0
    let overwritten = 0
    const details = []
    const dupItems = []
    try {
      for (let i = 0; i < queue.length; i += 1) {
        const item = queue[i]
        setProgress({ index: i, total: queue.length, percent: 0, name: item.name })
        let data
        try {
          data = await uploadOne(item, 'skip', (percent) =>
            setProgress({ index: i, total: queue.length, percent, name: item.name }),
          )
        } catch (err) {
          details.push({ name: item.name, status: 'error', message: err.message || '上传失败' })
          continue
        }
        if (data.batch && Array.isArray(data.results)) {
          // 多游戏包：展开每个子游戏结果
          data.results.forEach((r) => {
            const label = r.name || `${item.name}#${r.index}`
            if (r.status === 'ok') {
              ok += 1
              details.push({ name: label, status: 'ok', message: '导入成功' })
            } else if (r.status === 'overwritten') {
              ok += 1
              overwritten += 1
              details.push({ name: label, status: 'overwritten', message: r.message || '已覆盖' })
            } else if (r.status === 'skipped') {
              skipped += 1
              details.push({ name: label, status: 'skipped', message: r.message || '已跳过重复' })
              dupItems.push({
                idx: i, // 归属父队列项；多子项共享同一上传项
                name: label,
                existingId: r.existing && r.existing.id,
                existingName: r.existing && r.existing.name,
                reason: r.reason,
                childIndex: r.index, // 子游戏在包内的 ID，供 overwrite 时二次上传标识
              })
            } else {
              details.push({ name: label, status: 'error', message: r.message || '导入失败' })
            }
          })
        } else if (data.duplicate && data.skipped) {
          // 命中重复：暂不入库，收集待用户确认
          // 明细与弹窗项统一用同一展示名，保证「覆盖」后能回填该行状态
          const label = item.defaultName || item.name
          skipped += 1
          details.push({ name: label, status: 'skipped', message: data.message })
          dupItems.push({
            idx: i,
            name: label,
            existingId: data.existing && data.existing.id,
            existingName: data.existing && data.existing.name,
            reason: data.reason,
          })
        } else {
          ok += 1
          details.push({ name: item.name, status: 'ok', message: data.message })
        }
      }

      if (dupItems.length) {
        // 打开重复确认弹窗；暂存本轮结果，待用户决策后结算
        setDupDialog({ items: dupItems, _acc: { ok, skipped, overwritten, details } })
        return
      }
      // 无重复：直接结算
      finalize({ ok, skipped, overwritten, details })
      setQueue([])
      setClassified(false)
      reloadCats()
    } catch (e) {
      toast.error(e.message || '上传失败')
    } finally {
      setBusy(false)
      setProgress(null)
    }
  }

  // 结算：更新结果面板与 toast
  const finalize = ({ ok, skipped, overwritten, details }) => {
    const parts = [`成功 ${ok - overwritten} 个`]
    if (overwritten) parts.push(`覆盖 ${overwritten} 个`)
    if (skipped) parts.push(`跳过重复 ${skipped} 个`)
    setResult({ ok, skipped, overwritten, details })
    toast.success(`批量导入完成：${parts.join('，')}`)
    reloadCats()
  }

  // 弹窗确认：对勾选项二次上传覆盖，其余保持跳过
  // 注意：多游戏包子项共享同一父 zip；只要某父 zip 有任一子项被勾选，
  //       就以 overwrite 重传该 zip（会覆盖其全部重复子项）
  const confirmDupDialog = async (checkedIdxs) => {
    if (!dupDialog) return
    const { items, _acc } = dupDialog
    const acc = { ..._acc }
    const checkedSet = new Set(checkedIdxs)
    // 按父队列项分组：得到需要以 overwrite 重传的父项集合
    const parentIdxs = new Set()
    items.forEach((it) => {
      if (checkedSet.has(it.idx)) parentIdxs.add(it.idx)
    })
    setDupBusy(true)
    try {
      for (const pIdx of parentIdxs) {
        const item = queue[pIdx]
        if (!item) continue
        try {
          const data = await uploadOne(item, 'overwrite')
          // 多游戏包：逐个把被覆盖的子项回填
          if (data.batch && Array.isArray(data.results)) {
            data.results.forEach((r) => {
              if (r.status !== 'overwritten') return
              acc.overwritten += 1
              acc.ok += 1
              const d = acc.details.find((x) => x.name === (r.name || '') && x.status === 'skipped')
              if (d) {
                d.status = 'overwritten'
                d.message = r.message || '已覆盖'
              }
            })
          } else if (data.overwritten) {
            acc.overwritten += 1
            acc.ok += 1
            const it = items.find((x) => x.idx === pIdx)
            const d = acc.details.find(
              (x) => x.name === (it ? it.name : '') && x.status === 'skipped',
            )
            if (d) {
              d.status = 'overwritten'
              d.message = data.message || '已覆盖'
            }
          }
        } catch (err) {
          items
            .filter((x) => x.idx === pIdx)
            .forEach((it) => {
              const d = acc.details.find((x) => x.name === it.name && x.status === 'skipped')
              if (d) {
                d.status = 'error'
                d.message = err.message || '覆盖失败'
              }
            })
        }
      }
      setDupDialog(null)
      finalize(acc)
      setQueue([])
      setClassified(false)
    } finally {
      setDupBusy(false)
    }
  }

  // 全部跳过：直接结算（保持 skipped 计数）
  const skipAllDup = () => {
    if (!dupDialog) return
    const { _acc } = dupDialog
    setDupDialog(null)
    finalize({ ..._acc })
    setQueue([])
    setClassified(false)
  }

  // 【新增】白名单识别：点击 → 调用后端按白名单对已导入/待处理游戏自动分类
  const runClassify = async () => {
    setBusy(true)
    try {
      const data = await postJSON(API.gamesClassify, {})
      const updated = (data && data.updated) || []
      toast.success(`白名单识别完成，${updated.length} 个游戏已分类`)
      setClassified(true)
      reloadCats()
    } catch (e) {
      toast.error(e.message || '识别失败')
    } finally {
      setBusy(false)
    }
  }

  // 【新增】撤回识别：再次点击 → 撤销白名单分类结果
  const undoClassify = async () => {
    setBusy(true)
    try {
      const data = await postJSON(API.gamesClassifyUndo, {})
      const updated = (data && data.updated) || []
      toast.success(`已撤回 ${updated.length} 个游戏的分类`)
      setClassified(false)
      reloadCats()
    } catch (e) {
      toast.error(e.message || '撤回失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-black text-theme-text">导入处理</h2>
        <div className="flex gap-2">
          <button
            onClick={() => setMode('zip')}
            className={[
              'rounded-full px-3 py-1.5 text-xs font-bold',
              mode === 'zip' ? 'bg-theme-accent text-white' : 'bg-theme-panel text-theme-sub',
            ].join(' ')}
          >
            游戏包 (.zip)
          </button>
          <button
            onClick={() => setMode('source')}
            className={[
              'rounded-full px-3 py-1.5 text-xs font-bold',
              mode === 'source' ? 'bg-theme-accent text-white' : 'bg-theme-panel text-theme-sub',
            ].join(' ')}
          >
            源文件
          </button>
        </div>
      </div>

      {/* 上传区 */}
      <div className="rounded-2xl border-2 border-dashed border-theme-border bg-theme-panel p-6 text-center">
        <input
          ref={mode === 'zip' ? zipRef : romRef}
          type="file"
          accept={mode === 'zip' ? '.zip' : SOURCE_ACCEPT}
          multiple
          className="hidden"
          onChange={(e) => {
            const files = e.target.files
            if (!files || !files.length) return
            if (mode === 'zip') onPickZip(files)
            else onPickSource(files)
            e.target.value = ''
          }}
        />
        <Upload size={28} className="mx-auto mb-2 text-theme-sub" />
        <p className="mb-1 text-sm text-theme-text">
          {mode === 'zip'
            ? '请选择游戏包 (.zip)，压缩包根目录必须包含 config.json'
            : '请选择源文件（ROM / SWF / HTML），类型按扩展名自动识别，可手动修改；同名将被覆盖'}
        </p>
        <Button
          className="mt-2"
          onClick={() => (mode === 'zip' ? zipRef : romRef).current.click()}
        >
          添加文件
        </Button>
      </div>

      {/* 白名单识别按钮（新增功能核心交互） */}
      <div className="flex flex-wrap items-center gap-2 rounded-2xl border border-theme-border bg-theme-panel p-3">
        <ShieldCheck size={18} className="text-theme-accent" />
        <span className="text-sm font-bold text-theme-text">白名单分类</span>
        <span className="text-[11px] text-theme-sub">
          根据游戏名称匹配白名单词库自动归类
        </span>
        <div className="ml-auto">
          {classified ? (
            <Button variant="ghost" onClick={undoClassify} disabled={busy}>
              <RotateCcw size={14} /> 撤回识别
            </Button>
          ) : (
            <Button onClick={runClassify} disabled={busy}>
              <ShieldCheck size={14} /> 白名单识别
            </Button>
          )}
        </div>
      </div>

      {/* 上传进度条 */}
      {busy && progress && (
        <div className="space-y-1 rounded-2xl border border-theme-border bg-theme-panel p-3">
          <div className="flex items-center justify-between text-xs">
            <span className="truncate font-bold text-theme-text">
              正在上传（{progress.index + 1}/{progress.total}）：{progress.name}
            </span>
            <span className="ml-2 shrink-0 text-theme-sub">{progress.percent}%</span>
          </div>
          <div className="h-2 w-full overflow-hidden rounded-full bg-theme-border">
            <div
              className="h-full rounded-full bg-theme-accent transition-all duration-200"
              style={{ width: `${progress.percent}%` }}
            />
          </div>
        </div>
      )}

      {/* 待上传队列 */}
      {queue.length === 0 ? (
        <Empty
          text={
            mode === 'zip'
              ? '暂无待上传游戏包，请点击上方“添加”'
              : '暂无待上传源文件，请点击上方“添加”'
          }
        />
      ) : (
        <div className="space-y-2">
          {queue.map((item, idx) => (
            <div
              key={idx}
              className="flex flex-wrap items-center gap-2 rounded-xl border border-theme-border bg-theme-panel p-3"
            >
              <span className="min-w-[120px] flex-1 truncate text-sm font-bold text-theme-text">
                {item.name}
              </span>
              <Input
                className="max-w-[180px]"
                placeholder="游戏名称"
                value={item.defaultName}
                onChange={(e) => updateItem(idx, { defaultName: e.target.value })}
              />
              <Select
                className="max-w-[160px]"
                value={item.game_type}
                onChange={(e) => updateItem(idx, { game_type: e.target.value })}
              >
                {GAME_TYPES.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.value}
                  </option>
                ))}
              </Select>
              <Select
                className="max-w-[140px]"
                value={item.category_id}
                onChange={(e) => updateItem(idx, { category_id: e.target.value })}
              >
                <option value={0}>默认分类</option>
                {categories.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </Select>
              <button
                className="text-theme-sub hover:text-rose-500"
                onClick={() => removeItem(idx)}
              >
                <X size={16} />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* 最近一次导入结果 */}
      {result && (
        <div className="space-y-2 rounded-2xl border border-theme-border bg-theme-panel p-3">
          <div className="flex items-center gap-3 text-sm font-bold text-theme-text">
            <span>导入结果</span>
            <span className="text-theme-sub">
              成功 {result.ok - result.overwritten} · 覆盖 {result.overwritten} · 跳过重复{' '}
              {result.skipped}
            </span>
            <button
              className="ml-auto text-theme-sub hover:text-rose-500"
              onClick={() => setResult(null)}
            >
              <X size={16} />
            </button>
          </div>
          <div className="space-y-1">
            {result.details.map((d, i) => (
              <div key={i} className="flex items-center gap-2 text-xs">
                <span
                  className={[
                    'inline-block h-2 w-2 shrink-0 rounded-full',
                    d.status === 'ok'
                      ? 'bg-emerald-500'
                      : d.status === 'overwritten'
                        ? 'bg-amber-500'
                        : d.status === 'skipped'
                          ? 'bg-theme-sub'
                          : 'bg-rose-500',
                  ].join(' ')}
                />
                <span className="min-w-[120px] truncate font-bold text-theme-text">{d.name}</span>
                <span className="truncate text-theme-sub">{d.message}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={() => setQueue([])} disabled={busy}>
          取消并关闭
        </Button>
        <Button onClick={upload} disabled={busy || queue.length === 0}>
          {busy ? <Spinner className="h-4 w-4" /> : null}
          {busy && progress ? `上传中 ${progress.percent}%` : '开始上传'}
        </Button>
      </div>

      {/* 重复确认弹窗 */}
      <DuplicateDialog
        open={!!dupDialog}
        items={dupDialog ? dupDialog.items : []}
        busy={dupBusy}
        onConfirm={confirmDupDialog}
        onSkipAll={skipAllDup}
      />
    </div>
  )
}
