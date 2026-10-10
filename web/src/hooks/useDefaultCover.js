import { useEffect, useState } from 'react'
import { LS, lsGet, lsSet } from './usePersistence'

// 内置默认封面（无封面游戏使用），位于 web/public/images/default-covers/
// 顺序即"封面 1..5"，设置页按此顺序展示
export const DEFAULT_COVERS = [
  '/images/default-covers/1.png',
  '/images/default-covers/2.png',
  '/images/default-covers/3.png',
  '/images/default-covers/4.png',
  '/images/default-covers/5.png',
]

// 读者级映射变化事件：设置页修改后通知所有卡片即时刷新
const CHANGE_EVENT = 'uogame:default-cover-change'

// 依 id/名称生成稳定下标：同一游戏每次结果一致（避免随机封面频繁跳动）
function stableIndex(key, len) {
  const s = String(key == null ? '' : key)
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return len > 0 ? h % len : 0
}

// coverById 按封面序号（1..5）返回 URL；越界回退到第 1 张
export function coverById(id) {
  return DEFAULT_COVERS[id - 1] || DEFAULT_COVERS[0]
}

// readCoverMap 读取本地映射：{ [分类ID]: 封面序号(1..5) }
export function readCoverMap() {
  const m = lsGet(LS.defaultCoverMap, {})
  return m && typeof m === 'object' ? m : {}
}

// writeCoverMap 写入映射并广播变化
export function writeCoverMap(map) {
  lsSet(LS.defaultCoverMap, map || {})
  try {
    window.dispatchEvent(new Event(CHANGE_EVENT))
  } catch {
    /* ignore */
  }
}

// useCoverMap 订阅本地映射（设置页修改后，卡片自动重渲染）
export function useCoverMap() {
  const [map, setMap] = useState(readCoverMap)
  useEffect(() => {
    const onChange = () => setMap(readCoverMap())
    window.addEventListener(CHANGE_EVENT, onChange)
    return () => window.removeEventListener(CHANGE_EVENT, onChange)
  }, [])
  return map
}

// 取游戏的分类 ID 列表（优先多值 category_ids，回退单值 category_id）
function gameCategoryIds(game) {
  if (Array.isArray(game && game.category_ids) && game.category_ids.length) {
    return game.category_ids
  }
  return game && game.category_id ? [game.category_id] : []
}

// resolveDefaultCover 解析游戏应使用的默认封面：
// 1) 游戏所属分类若在映射中 → 用该分类指定的封面
// 2) 否则按 id/名称稳定随机选一张
export function resolveDefaultCover(game, map) {
  const m = map || readCoverMap()
  for (const cid of gameCategoryIds(game)) {
    const id = m[String(cid)]
    if (id >= 1 && id <= DEFAULT_COVERS.length) return coverById(id)
  }
  return coverById(stableIndex(game && (game.id != null ? game.id : game.name), DEFAULT_COVERS.length) + 1)
}
