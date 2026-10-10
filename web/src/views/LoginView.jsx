import React, { useState } from 'react'
import { Gamepad2 } from 'lucide-react'
import Button from '../components/ui/Button'
import Input from '../components/ui/Input'
import Modal from '../components/ui/Modal'
import { useAuth, getSavedUsername, setSavedUsername } from '../contexts/AuthContext'
import { useToast } from '../contexts/ToastContext'

// 登录 / 注册视图
// 设计：登录页只填账号密码；仅当同名账号同时存在于玩家表与管理员表时，才弹窗要求选择身份
export default function LoginView() {
  const { login, register } = useAuth()
  const toast = useToast()

  const [mode, setMode] = useState('login') // login | register
  const [username, setUsername] = useState(getSavedUsername())
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(!!getSavedUsername())
  const [busy, setBusy] = useState(false)
  // 同名账号同时存在于玩家表与管理员表 → 需用户指定登录身份
  const [needIdentity, setNeedIdentity] = useState(false)

  const doLogin = async (accountType) => {
    const data = await login(username.trim(), password, accountType)
    if (remember) setSavedUsername(username.trim())
    else setSavedUsername('')
    // 管理员登录后由外层弹出「进入身份」选择，此处不再重复提示
    if (!data || !data.user || data.user.account_type !== 'manager') {
      toast.success('登录成功')
    }
    return data
  }

  const submit = async (e) => {
    e.preventDefault()
    if (!username.trim() || !password) {
      toast.error('请填写完整信息')
      return
    }
    setBusy(true)
    try {
      if (mode === 'login') {
        await doLogin(undefined)
      } else {
        await register(username.trim(), password)
        toast.success('注册成功！请登录。')
        setMode('login')
      }
    } catch (err) {
      // 该用户名同时存在玩家与管理员账号 → 引导选择身份
      if (err && err.data && err.data.require_identity) {
        setNeedIdentity(true)
      } else {
        toast.error(err.message || '操作失败')
      }
    } finally {
      setBusy(false)
    }
  }

  // 选定身份后重新登录
  const chooseIdentity = async (accountType) => {
    setBusy(true)
    try {
      await doLogin(accountType)
      setNeedIdentity(false)
    } catch (err) {
      toast.error(err.message || '登录失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div className="flex min-h-full items-center justify-center p-6">
        <form
          onSubmit={submit}
          className="w-full max-w-sm rounded-3xl border border-theme-border bg-theme-panel p-6 shadow-xl"
        >
          <div className="mb-6 flex flex-col items-center gap-2">
            <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-theme-accent text-white">
              <Gamepad2 size={30} />
            </div>
            <h1 className="text-xl font-black tracking-widest text-theme-text">
              {mode === 'login' ? '欢迎回来' : '加入 LITE GAME'}
            </h1>
            <p className="text-xs text-theme-sub">
              {mode === 'login'
                ? '登录以同步您的存档与偏好'
                : '开启您的云端游戏体验'}
            </p>
          </div>

          <div className="space-y-3">
            <Input
              placeholder="用户名"
              value={username}
              autoComplete="username"
              onChange={(e) => setUsername(e.target.value)}
            />
            <Input
              type="password"
              placeholder="密码"
              value={password}
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              onChange={(e) => setPassword(e.target.value)}
            />

            {mode === 'login' && (
              <label className="flex items-center gap-2 text-xs text-theme-sub">
                <input
                  type="checkbox"
                  checked={remember}
                  onChange={(e) => setRemember(e.target.checked)}
                />
                记住我的身份
              </label>
            )}
          </div>

          <Button type="submit" className="mt-5 w-full" disabled={busy}>
            {busy ? '处理中...' : mode === 'login' ? '开启游戏' : '立即加入'}
          </Button>

          <button
            type="button"
            className="mt-4 w-full text-center text-xs text-theme-sub hover:text-theme-accent"
            onClick={() => {
              setMode(mode === 'login' ? 'register' : 'login')
              setNeedIdentity(false)
            }}
          >
            {mode === 'login' ? '没有账号？创建一个' : '已有账号？点此登录'}
          </button>
        </form>
      </div>

      <Modal
        open={needIdentity}
        title="选择登录身份"
        onClose={() => setNeedIdentity(false)}
      >
        <p className="mb-3 text-theme-sub">
          该用户名同时存在玩家与管理员账号，请选择要登录的身份。
        </p>
        <div className="flex flex-col gap-2">
          <Button onClick={() => chooseIdentity('player')} disabled={busy}>
            以玩家身份登录
          </Button>
          <Button
            variant="ghost"
            onClick={() => chooseIdentity('manager')}
            disabled={busy}
          >
            以管理员身份登录
          </Button>
        </div>
      </Modal>
    </>
  )
}