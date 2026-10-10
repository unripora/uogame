// 游戏源管理（阅读式源模式）
//
// 设计：
//   - 「本地源」为内置源，指向同源后端，不可删除，始终排在首位；
//   - 「远程源」由用户添加（源描述 source.json），持久化到 localStorage；
//   - 每个源解析出绝对地址（resolveSourceUrl），供 API / 播放统一拼接；
//   - 源描述遵循协议 uogame-source（见后端 source.go / I_solutions.md 第八节）。
//
// 本地存储键：
//   uogame_sources      远程源列表（JSON 数组）
//   uogame_current_source  当前源 id（用于浏览上下文）

import { lsGet, lsSet, lsGetStr, lsSetStr } from '../hooks/usePersistence'

export const SOURCES_KEY = 'uogame_sources'
export const CURRENT_SOURCE_KEY = 'uogame_current_source'

// 本地源固定标识与伪地址（local:// 表示同源后端）
export const LOCAL_SOURCE_ID = 'local'
export const LOCAL_BASE_URL = 'local://'

// 源协议常量
export const SOURCE_PROTOCOL = 'uogame-source'
export const SOURCE_VERSION = 1

// 本地源（内置，不可删除）
export const LOCAL_SOURCE = {
  id: LOCAL_SOURCE_ID,
  name: '本机游戏库',
  baseUrl: LOCAL_BASE_URL,
  protocol: SOURCE_PROTOCOL,
  version: SOURCE_VERSION,
  capabilities: {
    browse: true,
    search: true,
    detail: true,
    stream: true,
    offline: false,
    local: true,
  },
  endpoints: {
    manifest: '/api/source/manifest',
    search: '/api/source/search',
    detail: '/api/source/game/{id}',
    cover: '/api/games/{id}/cover',
    images: '/api/games/{id}/images',
    stream: '/play/{id}/{file}',
    login: '/api/auth/login',
  },
  auth: { type: 'bearer' },
}

// 读取用户添加的远程源
export function getRemoteSources() {
  const list = lsGet(SOURCES_KEY, [])
  return Array.isArray(list) ? list : []
}

// 全部源 = 本地源 + 远程源
export function getAllSources() {
  return [LOCAL_SOURCE, ...getRemoteSources()]
}

// 按 id 取源（未命中回退本地源）
export function getSourceById(id) {
  if (!id || id === LOCAL_SOURCE_ID) return LOCAL_SOURCE
  return getRemoteSources().find((s) => s.id === id) || LOCAL_SOURCE
}

// 当前源 id（缺省为本地源）
export function getCurrentSourceId() {
  return lsGetStr(CURRENT_SOURCE_KEY, LOCAL_SOURCE_ID) || LOCAL_SOURCE_ID
}

// 设置当前源
export function setCurrentSourceId(id) {
  lsSetStr(CURRENT_SOURCE_KEY, id || LOCAL_SOURCE_ID)
}

// 当前源对象
export function getCurrentSource() {
  return getSourceById(getCurrentSourceId())
}

// 生成远程源 id：名称 + baseUrl 的稳定短哈希
export function makeSourceId(name, baseUrl) {
  const raw = `${name || ''}|${baseUrl || ''}`
  let hash = 0
  for (let i = 0; i < raw.length; i++) {
    hash = (hash * 31 + raw.charCodeAt(i)) | 0
  }
  return `src_${Math.abs(hash).toString(36)}`
}

// 保存远程源列表
export function saveRemoteSources(list) {
  lsSet(SOURCES_KEY, Array.isArray(list) ? list : [])
}

// 新增/更新一个远程源（按 id 去重），返回保存后的列表
export function upsertRemoteSource(source) {
  const list = getRemoteSources()
  const id = source.id || makeSourceId(source.name, source.baseUrl)
  const normalized = { ...source, id }
  const idx = list.findIndex((s) => s.id === id)
  if (idx >= 0) list[idx] = normalized
  else list.push(normalized)
  saveRemoteSources(list)
  return list
}

// 删除一个远程源（本地源不可删）
export function removeRemoteSource(id) {
  if (id === LOCAL_SOURCE_ID) return getRemoteSources()
  const list = getRemoteSources().filter((s) => s.id !== id)
  saveRemoteSources(list)
  // 若删除的是当前源，回退到本地源
  if (getCurrentSourceId() === id) setCurrentSourceId(LOCAL_SOURCE_ID)
  return list
}

// 解析源地址：把相对路径拼到源的 baseUrl 上
//   - 本地源（local://）→ 返回相对路径（同源）
//   - 远程源 → 返回 baseUrl + path 的绝对地址
export function resolveSourceUrl(source, path) {
  const src = source || getCurrentSource()
  const p = path || ''
  if (!src || !src.baseUrl || src.baseUrl === LOCAL_BASE_URL) {
    return p
  }
  const base = src.baseUrl.replace(/\/+$/, '')
  const rel = p.startsWith('/') ? p : `/${p}`
  return `${base}${rel}`
}

// 端点模板填充：把 {id} / {file} 等占位符替换为实际值
export function fillEndpoint(template, params = {}) {
  if (!template) return ''
  return template.replace(/\{(\w+)\}/g, (_, key) =>
    params[key] != null ? encodeURIComponent(params[key]) : ''
  )
}

// 当前源的某个端点绝对地址（便捷封装）
export function currentEndpoint(key, params) {
  const src = getCurrentSource()
  const tpl = src?.endpoints?.[key]
  return resolveSourceUrl(src, fillEndpoint(tpl, params))
}

// 校验并规范化一份源描述（用户粘贴的 source.json）
// 返回 { ok, source, error }
export function normalizeSourceDescriptor(desc) {
  if (!desc || typeof desc !== 'object') {
    return { ok: false, error: '源描述不是合法对象' }
  }
  if (desc.protocol !== SOURCE_PROTOCOL) {
    return { ok: false, error: `协议不匹配（期望 ${SOURCE_PROTOCOL}）` }
  }
  if (Number(desc.version) !== SOURCE_VERSION) {
    return { ok: false, error: `协议版本不匹配（期望 ${SOURCE_VERSION}）` }
  }
  if (!desc.baseUrl) {
    return { ok: false, error: '缺少 baseUrl' }
  }
  const source = {
    id: makeSourceId(desc.name, desc.baseUrl),
    name: desc.name || desc.baseUrl,
    baseUrl: desc.baseUrl,
    protocol: desc.protocol,
    version: Number(desc.version),
    icon: desc.icon || '',
    capabilities: desc.capabilities || {},
    endpoints: desc.endpoints || {},
    auth: desc.auth || { type: 'bearer' },
  }
  return { ok: true, source }
}

// 发现：根据用户填写的域名拉取 /.well-known/uogame-source.json
// 返回规范化后的 { ok, source, error }
export async function discoverSource(input) {
  let base = (input || '').trim()
  if (!base) return { ok: false, error: '请输入源地址' }
  if (!/^https?:\/\//i.test(base)) base = `https://${base}`
  base = base.replace(/\/+$/, '').replace(/\/\.well-known\/uogame-source\.json$/i, '')
  const url = `${base}/.well-known/uogame-source.json`
  try {
    const res = await fetch(url, { credentials: 'omit' })
    if (!res.ok) return { ok: false, error: `源描述请求失败（${res.status}）` }
    const desc = await res.json()
    // 若源未自带 baseUrl，用发现地址补齐
    if (!desc.baseUrl) desc.baseUrl = base
    return normalizeSourceDescriptor(desc)
  } catch (e) {
    return { ok: false, error: `无法连接源：${e.message || '网络错误'}` }
  }
}
