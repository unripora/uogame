import React, { useState } from 'react'
import { Plus, Trash2, Pencil } from 'lucide-react'
import Button from '../../components/ui/Button'
import Input from '../../components/ui/Input'
import Spinner from '../../components/ui/Spinner'
import Empty from '../../components/ui/Empty'
import { useCategories } from '../../hooks/useCategories'
import { useToast } from '../../contexts/ToastContext'
import { API } from '../../api/endpoints'
import { postJSON, putJSON, del, getJSON } from '../../api/client'

// 分类配置面板：列表 + 新增 + 编辑 + 删除
export default function CategoryPanel() {
  const { categories, reload, loading } = useCategories()
  const toast = useToast()
  const [name, setName] = useState('')
  const [editing, setEditing] = useState(null)
  const [busy, setBusy] = useState(false)

  const create = async () => {
    if (!name.trim()) {
      toast.error('名称不能为空')
      return
    }
    setBusy(true)
    try {
      await postJSON(API.categories, { name: name.trim(), sort_order: categories.length + 1 })
      toast.success('新增分类')
      setName('')
      await reload()
    } catch (e) {
      toast.error(e.message || '操作失败')
    } finally {
      setBusy(false)
    }
  }

  const saveEdit = async () => {
    if (!editing || !editing.name.trim()) return
    try {
      await putJSON(API.category(editing.id), { name: editing.name.trim() })
      toast.success('修改')
      setEditing(null)
      await reload()
    } catch (e) {
      toast.error(e.message || '保存失败')
    }
  }

  const remove = async (cat) => {
    if (!confirm(`警告：删除后，该分类下的游戏将自动变为“默认分类”。确定继续吗？`)) return
    try {
      await del(API.category(cat.id))
      toast.success('分类删除成功')
      await reload()
    } catch (e) {
      toast.error(e.message || '操作失败')
    }
  }

  return (
    <div className="space-y-4">
      <h2 className="text-base font-black text-theme-text">游戏分类配置</h2>
      <div className="flex gap-2">
        <Input
          placeholder="输入分类名称"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <Button onClick={create} disabled={busy}>
          <Plus size={16} /> 新增分类
        </Button>
      </div>

      {loading && categories.length === 0 ? (
        <div className="flex justify-center py-8">
          <Spinner />
        </div>
      ) : categories.length === 0 ? (
        <Empty text="暂无分类" />
      ) : (
        <div className="space-y-2">
          {categories.map((c) => (
            <div
              key={c.id}
              className="flex items-center gap-2 rounded-xl border border-theme-border bg-theme-panel p-2"
            >
              {editing && editing.id === c.id ? (
                <>
                  <Input
                    value={editing.name}
                    onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                  />
                  <Button onClick={saveEdit}>保存</Button>
                  <Button variant="ghost" onClick={() => setEditing(null)}>
                    取消
                  </Button>
                </>
              ) : (
                <>
                  <span className="flex-1 text-sm font-bold text-theme-text">
                    {c.name}
                  </span>
                  <button
                    className="text-theme-sub hover:text-theme-accent"
                    onClick={() => setEditing({ id: c.id, name: c.name })}
                    title="编辑分类"
                  >
                    <Pencil size={16} />
                  </button>
                  <button
                    className="text-theme-sub hover:text-rose-500"
                    onClick={() => remove(c)}
                    title="删除分类"
                  >
                    <Trash2 size={16} />
                  </button>
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
