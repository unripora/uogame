// 统一请求封装
// 鉴权三通道（与后端 AuthMiddleware 对齐）：
//   1) HttpOnly Cookie（credentials: 'include'）
//   2) Authorization: Bearer <token>（localStorage: uogame_token）
//   3) ?token= 查询参数（用于导出等 iframe/直链场景）
//
// 源模式：所有以 `/` 开头的相对地址会按「当前源」的 baseUrl 解析为绝对地址，
// 从而让同一前端可指向不同源（本地源 = 同源）。

import { resolveSourceUrl } from '../lib/source'

export const TOKEN_KEY = 'uogame_token'

export function getToken() {
  try {
    return localStorage.getItem(TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

export function setToken(token) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* ignore */
  }
}

// 兼容产物中的 `t()` 请求函数：返回原生 Response
export async function request(url, options = {}) {
  const opts = { credentials: 'include', ...options }
  const headers = { ...(options.headers || {}) }
  const token = getToken()
  if (token && !headers.Authorization) {
    headers.Authorization = `Bearer ${token}`
  }
  opts.headers = headers
  return fetch(resolveUrl(url), opts)
}

// 把相对地址按当前源解析为绝对地址（已是绝对地址则原样返回）
// 说明：resolveSourceUrl 导入自 lib/source，避免循环依赖（source 不引 client）
// 范围收窄：仅游戏播放路径（/play/）按当前源解析；
// 其余以 / 开头的路径（/api/* 等）一律同源，避免静态源激活时后端请求被拼到 Pages 域名
export function resolveUrl(url) {
  if (!url || typeof url !== 'string') return url
  if (/^(https?:)?\/\//i.test(url) || url.startsWith('data:') || url.startsWith('blob:')) {
    return url
  }
  if (url.startsWith('/play/')) return resolveSourceUrl(null, url)
  return url
}

// 带上传进度的请求：基于 XMLHttpRequest（fetch 无法获取上传进度）
// 参数：
//   url      - 请求地址
//   formData - FormData 实例
//   onProgress(loaded, total, percent) - 上传进度回调（percent 为 0-100 整数）
//   timeout  - 超时毫秒（默认 10 分钟，避免大文件上传被误判为卡死）
// 返回：与 fetch Response 兼容的对象 { ok, status, json(), text() }
export function uploadWithProgress(url, formData, { onProgress, timeout = 600000 } = {}) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', resolveUrl(url), true)
    xhr.withCredentials = true
    xhr.timeout = timeout
    const token = getToken()
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)

    // 上传进度
    if (xhr.upload && typeof onProgress === 'function') {
      xhr.upload.onprogress = (e) => {
        if (!e.lengthComputable) return
        const percent = Math.min(100, Math.round((e.loaded / e.total) * 100))
        onProgress(e.loaded, e.total, percent)
      }
    }

    xhr.onload = () => {
      const text = xhr.responseText || ''
      resolve({
        ok: xhr.status >= 200 && xhr.status < 300,
        status: xhr.status,
        text: () => Promise.resolve(text),
        json: () => {
          try {
            return Promise.resolve(JSON.parse(text))
          } catch {
            return Promise.reject(new Error('响应不是合法 JSON'))
          }
        },
      })
    }
    xhr.onerror = () => reject(new Error('网络错误，上传失败'))
    xhr.ontimeout = () => reject(new Error('上传超时，请检查网络后重试'))
    xhr.onabort = () => reject(new Error('上传已取消'))
    xhr.send(formData)
  })
}

// 便捷 JSON 请求：自动解析、非 2xx 抛错（错误信息优先用响应体 error 字段）
export async function requestJSON(url, options = {}) {
  const res = await request(url, options)
  let data = null
  const text = await res.text()
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }
  if (!res.ok) {
    const msg =
      (data && data.error) || `请求失败 (${res.status})`
    const err = new Error(msg)
    err.status = res.status
    err.data = data
    throw err
  }
  return data
}

export function getJSON(url) {
  return requestJSON(url)
}

export function postJSON(url, body) {
  return requestJSON(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  })
}

export function putJSON(url, body) {
  return requestJSON(url, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  })
}

export function del(url) {
  return requestJSON(url, { method: 'DELETE' })
}
