// 游戏源管理（阅读式源模式）
//
// 设计：
//   - 「本地源」为内置源，指向同源后端，不可删除，始终排在首位（type=dynamic）；
//   - 「远程源」由用户添加（源描述 source.json），持久化到 localStorage；
//   - 源有两种类型：
//       dynamic —— 走 API（manifest/search/detail 接口）
//       static  —— 走静态托管（GitHub Pages），清单一次性拉取 + 前端本地搜索
//   - 每个源解析出绝对地址（resolveSourceUrl），供 API / 播放统一拼接；
//   - 源描述遵循协议 uogame-source（见后端 source.go / README）。
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

// 源类型
export const SOURCE_TYPE_DYNAMIC = 'dynamic'
export const SOURCE_TYPE_STATIC = 'static'

// 本地源（内置，不可删除；动态源）
export const LOCAL_SOURCE = {
  id: LOCAL_SOURCE_ID,
  name: '本机游戏库',
  baseUrl: LOCAL_BASE_URL,
  protocol: SOURCE_PROTOCOL,
  version: SOURCE_VERSION,
  type: SOURCE_TYPE_DYNAMIC,
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

// 判断源是否为静态源
export function isStaticSource(source) {
  return !!source && source.type === SOURCE_TYPE_STATIC
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
  const type = desc.type === SOURCE_TYPE_STATIC ? SOURCE_TYPE_STATIC : SOURCE_TYPE_DYNAMIC
  // 静态源必须声明 manifest 文件名
  if (type === SOURCE_TYPE_STATIC && !desc.manifest) {
    return { ok: false, error: '静态源缺少 manifest 字段' }
  }
  const source = {
    id: makeSourceId(desc.name, desc.baseUrl),
    name: desc.name || desc.baseUrl,
    baseUrl: desc.baseUrl,
    protocol: desc.protocol,
    version: Number(desc.version),
    type,
    icon: desc.icon || '',
    // 静态源：manifest 相对路径
    manifest: desc.manifest || '',
    capabilities: desc.capabilities || {},
    endpoints: desc.endpoints || {},
    auth: desc.auth || { type: 'bearer' },
  }
  return { ok: true, source }
}

// 归一化用户输入为 baseUrl（去尾斜杠、去已知发现路径）
function normalizeBaseUrl(input) {
  let base = (input || '').trim()
  if (!base) return ''
  if (!/^https?:\/\//i.test(base)) base = `https://${base}`
  return base
    .replace(/\/+$/, '')
    .replace(/\/\.well-known\/uogame-source\.json$/i, '')
}

// 发现：兼容动态源与静态源
//   1) 先试动态源：baseUrl/.well-known/uogame-source.json
//   2) 失败则试静态源：把输入当作 uogame-source.json 的直链
//   3) 再失败则试 baseUrl/uogame-source.json
// 返回规范化后的 { ok, source, error }
export async function discoverSource(input) {
  const raw = (input || '').trim()
  if (!raw) return { ok: false, error: '请输入源地址' }

  // 情况：用户直接贴了 xxx/uogame-source.json 的完整链接
  if (/uogame-source\.json$/i.test(raw)) {
    const url = /^https?:\/\//i.test(raw) ? raw : `https://${raw}`
    const r = await tryFetchDescriptor(url)
    if (r.ok) return r
    return { ok: false, error: r.error }
  }

  const base = normalizeBaseUrl(raw)

  // 1) 动态源发现
  const dyn = await tryFetchDescriptor(`${base}/.well-known/uogame-source.json`)
  if (dyn.ok) return dyn

  // 2) 静态源：baseUrl/uogame-source.json
  const stat = await tryFetchDescriptor(`${base}/uogame-source.json`)
  if (stat.ok) {
    // 静态源若未自带 baseUrl，用推测的 base 补齐
    return stat
  }

  return { ok: false, error: `无法发现源：${dyn.error}` }
}

// 拉取并规范化一份源描述（内部工具）
async function tryFetchDescriptor(url) {
  try {
    const res = await fetch(url, { credentials: 'omit' })
    if (!res.ok) return { ok: false, error: `源描述请求失败（${res.status}）` }
    const desc = await res.json()
    // 静态源：baseUrl 缺省时，用该描述文件所在目录补齐
    if (!desc.baseUrl && desc.type === SOURCE_TYPE_STATIC) {
      desc.baseUrl = url.replace(/\/uogame-source\.json.*$/i, '')
    }
    return normalizeSourceDescriptor(desc)
  } catch (e) {
    return { ok: false, error: `无法连接源：${e.message || '网络错误'}` }
  }
}
