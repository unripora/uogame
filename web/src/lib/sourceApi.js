// 源接口客户端（阅读式源模式）
//
// 与 api/client 的区别：
//   - client.js 面向「当前源」的常规业务请求（自动前缀）；
//   - 本模块面向「任意指定源」的元数据拉取，用于多源聚合搜索与浏览。
//
// 每个方法的第一个参数均为「源对象」，便于跨源并发调用。

import { resolveSourceUrl, fillEndpoint } from './source'
import { getToken } from '../api/client'

// 带源的 fetch：自动拼接绝对地址 + 注入 Bearer token
async function sourceFetch(source, path, options = {}) {
  const url = resolveSourceUrl(source, path)
  const headers = { ...(options.headers || {}) }
  const token = getToken()
  if (token && !headers.Authorization) headers.Authorization = `Bearer ${token}`
  const res = await fetch(url, { credentials: 'include', ...options, headers })
  if (!res.ok) throw new Error(`请求失败（${res.status}）`)
  return res.json()
}

// 拉取某个源的清单（分页）
// 返回 { items, total, page, page_size }
export async function fetchManifest(source, { page = 1, pageSize = 50 } = {}) {
  const tpl = source?.endpoints?.manifest || '/api/source/manifest'
  const path = `${fillEndpoint(tpl, {})}?page=${page}&page_size=${pageSize}`
  const data = await sourceFetch(source, path)
  return normalizeManifest(data, source)
}

// 在某个源上搜索
export async function searchSource(source, q) {
  const tpl = source?.endpoints?.search || '/api/source/search'
  const path = `${fillEndpoint(tpl, {})}?q=${encodeURIComponent(q || '')}`
  const data = await sourceFetch(source, path)
  return normalizeManifest(data, source)
}

// 拉取某个源上的游戏详情
export async function fetchSourceDetail(source, id) {
  const tpl = source?.endpoints?.detail || '/api/source/game/{id}'
  const path = fillEndpoint(tpl, { id })
  const data = await sourceFetch(source, path)
  return attachSource(data, source)
}

// 规范化清单返回，并给每个条目附加来源标记
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
