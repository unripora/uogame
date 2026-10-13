// 源接口客户端（阅读式源模式）
//
// 与 api/client 的区别：
//   - client.js 面向「当前源」的常规业务请求（自动前缀）；
//   - 本模块面向「任意指定源」的元数据拉取，用于多源聚合搜索与浏览。
//
// 每个方法的第一个参数均为「源对象」，便于跨源并发调用。
//
// 两类源分流：
//   - 动态源（dynamic）：走 API（manifest / search / detail 端点）
//   - 静态源（static）：一次性拉取 manifest.json，搜索走前端本地过滤

import { resolveSourceUrl, fillEndpoint, isStaticSource } from './source'
import { getToken } from '../api/client'

// 带源的 fetch：自动拼接绝对地址 + 注入 Bearer token
// 静态源例外：GitHub Pages 等公开托管返回 Access-Control-Allow-Origin: *，
// 不允许携带凭据（credentials: 'include'）或自定义头（会触发预检并失败），
// 因此静态源一律匿名请求（credentials: 'omit'，不带 Authorization）
async function sourceFetch(source, path, options = {}) {
  const url = resolveSourceUrl(source, path)
  const headers = { ...(options.headers || {}) }
  if (isStaticSource(source)) {
    const res = await fetch(url, { ...options, headers })
    if (!res.ok) throw new Error(`请求失败（${res.status}）`)
    return res.json()
  }
  const token = getToken()
  if (token && !headers.Authorization) headers.Authorization = `Bearer ${token}`
  const res = await fetch(url, { credentials: 'include', ...options, headers })
  if (!res.ok) throw new Error(`请求失败（${res.status}）`)
  return res.json()
}

// 静态源清单缓存：同一源在会话内只全量拉取一次（manifest 不常变）
const staticManifestCache = new Map() // source.id -> { items, total, at }

// 拉取某个源的清单
//   - 动态源：分页接口
//   - 静态源：拉 manifest.json（全量），本地分页
// 统一返回 { items, total, page, page_size }
export async function fetchManifest(source, { page = 1, pageSize = 50 } = {}) {
  if (isStaticSource(source)) {
    const all = await loadStaticManifest(source)
    const start = (page - 1) * pageSize
    return {
      items: all.slice(start, start + pageSize),
      total: all.length,
      page,
      pageSize,
    }
  }
  const tpl = source?.endpoints?.manifest || '/api/source/manifest'
  const path = `${fillEndpoint(tpl, {})}?page=${page}&page_size=${pageSize}`
  const data = await sourceFetch(source, path)
  return normalizeManifest(data, source)
}

// 在某个源上搜索
//   - 动态源：调用 search 端点
//   - 静态源：本地关键词过滤（主名 + 别名 + 标签）
export async function searchSource(source, q) {
  const kw = (q || '').trim()
  if (isStaticSource(source)) {
    const all = await loadStaticManifest(source)
    if (!kw) return { items: all, total: all.length, page: 1, pageSize: all.length }
    const lower = kw.toLowerCase()
    const hit = all.filter((g) => {
      if ((g.name || '').toLowerCase().includes(lower)) return true
      if (Array.isArray(g.aliases) && g.aliases.some((a) => (a || '').toLowerCase().includes(lower)))
        return true
      if (Array.isArray(g.tags) && g.tags.some((t) => (t || '').toLowerCase().includes(lower)))
        return true
      return false
    })
    return { items: hit, total: hit.length, page: 1, pageSize: hit.length }
  }
  const tpl = source?.endpoints?.search || '/api/source/search'
  const path = `${fillEndpoint(tpl, {})}?q=${encodeURIComponent(kw)}`
  const data = await sourceFetch(source, path)
  return normalizeManifest(data, source)
}

// 拉取某个源上的游戏详情
//   - 动态源：detail 端点
//   - 静态源：details/{id}.json（可选，缺失时回退清单内已含字段）
export async function fetchSourceDetail(source, id) {
  if (isStaticSource(source)) {
    const list = await loadStaticManifest(source)
    const base = list.find((g) => String(g.id) === String(id))
    if (!base) throw new Error('未找到该游戏')
    // 详情分片可选：失败则返回列表字段
    try {
      const detailPath = `details/${encodeURIComponent(id)}.json`
      const extra = await sourceFetch(source, detailPath)
      return { ...base, ...extra }
    } catch {
      return base
    }
  }
  const tpl = source?.endpoints?.detail || '/api/source/game/{id}'
  const path = fillEndpoint(tpl, { id })
  const data = await sourceFetch(source, path)
  return attachSource(data, source)
}

// 加载静态源清单（带会话内缓存 + 规范化）
async function loadStaticManifest(source) {
  const cached = staticManifestCache.get(source.id)
  if (cached) return cached.items
  const manifestPath = source.manifest || 'manifest.json'
  const data = await sourceFetch(source, manifestPath)
  // 兼容 { games: [...] } / { items: [...] } / 裸数组
  const raw = Array.isArray(data) ? data : data?.games || data?.items || []
  const items = raw.map((it) => normalizeStaticGame(it, source))
  staticManifestCache.set(source.id, { items, total: items.length, at: Date.now() })
  return items
}

// 静态源条目规范化：统一为 SourceGame 结构 + 附加播放所需信息
function normalizeStaticGame(it, source) {
  const path = it.path || ''
  return {
    id: it.id,
    name: it.name || '',
    game_type: it.game_type || 'H5',
    platform_tag: it.platform_tag || '',
    cover: it.cover ? resolveSourceUrl(source, it.cover) : '',
    tags: Array.isArray(it.tags) ? it.tags : [],
    aliases: Array.isArray(it.aliases) ? it.aliases : [],
    description: it.description || '',
    release_date: it.release_date || '',
    size_bytes: it.size_bytes || 0,
    avg_rating: it.avg_rating || 0,
    rating_count: it.rating_count || 0,
    // 静态源播放所需：入口文件与路径（PlayView 用）
    path,
    entry: it.entry || 'index.html',
    _source: { id: source.id, name: source.name },
    _static: true,
  }
}

// 规范化动态源清单返回，并给每个条目附加来源标记
function normalizeManifest(data, source) {
  // 兼容：后端返回 { items, total } 或裸数组
  const items = Array.isArray(data) ? data : data?.items || []
  const total = Array.isArray(data) ? items.length : data?.total ?? items.length
  return {
    total,
    page: data?.page ?? 1,
    pageSize: data?.page_size ?? items.length,
    items: items.map((it) => attachSource(it, source)),
  }
}

// 给条目附加来源信息（不改变原字段）
function attachSource(item, source) {
  return { ...item, _source: { id: source.id, name: source.name } }
}

// 跨源聚合搜索：并行查询所有源，合并结果
//   - 单个源失败不影响整体（allSettled）
//   - 返回 { items, errors: [{ source, error }] }
export async function searchAllSources(sources, q) {
  const results = await Promise.allSettled(
    (sources || []).map((s) => searchSource(s, q))
  )
  const items = []
  const errors = []
  results.forEach((r, i) => {
    const src = sources[i]
    if (r.status === 'fulfilled') {
      items.push(...r.value.items)
    } else {
      errors.push({ source: src, error: r.reason })
    }
  })
  return { items, errors }
}

// 清空静态源缓存（切换/删除源时调用，确保数据新鲜）
export function clearStaticManifestCache(sourceId) {
  if (sourceId) staticManifestCache.delete(sourceId)
  else staticManifestCache.clear()
}
