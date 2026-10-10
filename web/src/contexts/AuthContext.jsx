import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { API } from '../api/endpoints'
import { getJSON, postJSON, requestJSON, setToken } from '../api/client'
import { LS, lsGet, lsGetStr, lsRemove, lsSet, lsSetStr } from '../hooks/usePersistence'

// 认证上下文：登录态、用户信息、校验、登出
// 关键契约（后端已对齐）：
//   - POST /api/auth/login  → { message, user, token }
//   - GET  /api/auth/verify → { authenticated: true, user }
//   - POST /api/auth/logout
const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [user, setUser] = useState(() => lsGet(LS.user, null))
  const [switchIdentity, setSwitchIdentity] = useState(null)
  // 登录识别为管理员后，待用户选择「进入身份」的管理员对象
  const [identityPrompt, setIdentityPrompt] = useState(null)
  const [loading, setLoading] = useState(true)

  // 启动时校验登录态（刷新保持登录的关键）
  useEffect(() => {
    let alive = true
    ;(async () => {
      try {
        const data = await getJSON(API.authVerify)
        if (!alive) return
        if (data && data.authenticated) {
          setUser(data.user || null)
          setSwitchIdentity(data.switch_identity || null)
          lsSet(LS.user, data.user || null)
        } else {
          setUser(null)
          setSwitchIdentity(null)
          lsRemove(LS.user)
        }
      } catch {
        if (!alive) return
        setUser(null)
        setSwitchIdentity(null)
        lsRemove(LS.user)
      } finally {
        if (alive) setLoading(false)
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  // accountType 可选：player / manager；同名两表都存在时后端返回 409 require_identity
  const login = useCallback(async (username, password, accountType) => {
    const body = { username, password }
    if (accountType) body.account_type = accountType
    const data = await postJSON(API.authLogin, body)
    if (data && data.token) setToken(data.token)
    if (data && data.user) {
      setUser(data.user)
      lsSet(LS.user, data.user)
      // 登录即识别为管理员 → 交由外层弹出「进入身份」选择
      setIdentityPrompt(data.user.account_type === 'manager' ? data.user : null)
    }
    return data
  }, [])

  const register = useCallback(async (username, password) => {
    return postJSON(API.authRegister, { username, password })
  }, [])

  // 一键切换身份（基于人员绑定，免密）
  const switchTo = useCallback(async (targetType) => {
    const data = await postJSON(API.userSwitchIdentity, { target_type: targetType })
    if (data && data.token) setToken(data.token)
    if (data && data.user) {
      setUser(data.user)
      lsSet(LS.user, data.user)
    }
    // 切换后重新校验，刷新可切换身份信息
    try {
      const v = await getJSON(API.authVerify)
      if (v && v.authenticated) {
        setUser(v.user || null)
        lsSet(LS.user, v.user || null)
        setSwitchIdentity(v.switch_identity || null)
      }
    } catch {
      /* 忽略校验失败 */
    }
    return data
  }, [])

  const logout = useCallback(async () => {
    try {
      await requestJSON(API.authLogout, { method: 'POST' })
    } catch {
      /* 忽略登出错误 */
    }
    setToken('')
    lsSetStr(LS.token, '')
    lsRemove(LS.user)
    setUser(null)
    setSwitchIdentity(null)
    setIdentityPrompt(null)
  }, [])

  // 关闭「进入身份」选择弹窗
  const clearIdentityPrompt = useCallback(() => setIdentityPrompt(null), [])

  const refreshUser = useCallback((u) => {
    setUser(u)
    lsSet(LS.user, u)
  }, [])

  // 仅管理员体系账号可进入后台
  const isManager = !!(user && user.account_type === 'manager')
  // admin 与 super_admin 均视为管理员（super_admin 拥有全部权限）
  const isAdmin =
    isManager && !!(user && (user.role === 'admin' || user.role === 'super_admin'))

  return (
    <AuthContext.Provider
      value={{
        user,
        loading,
        login,
        register,
        logout,
        isAdmin,
        isManager,
        switchIdentity,
        switchTo,
        identityPrompt,
        clearIdentityPrompt,
        refreshUser,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

// 记住的用户名（产物 uogame_saved_username）
export function getSavedUsername() {
  return lsGetStr(LS.savedUsername, '')
}
export function setSavedUsername(name) {
  lsSetStr(LS.savedUsername, name)
}
