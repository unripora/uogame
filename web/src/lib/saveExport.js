// 游戏存档导出：读取本机存档并组装为**可移植** JSON（v2）下载。
//
// 数据源与归属：
//   1) ROM 存档 — IndexedDB `uogameSavesDB/saves`，键 `uogame_save_<gameId>_<romName>`，值 Uint8Array
//   2) H5 存档 — 方案 B（服务端注入命名空间）后：
//        localStorage：键 `h5g<id>:<key>`
//        IndexedDB ：库名  `h5g<id>::<db>`
// 说明：localStorage 中的同名 base64 只是兜底且会被配额清理，故不作为数据源。
//
// 跨机可移植性（v2 新增，供 saveImport 按游戏名映射）：
//   - 顶层 `games`：{ 导出时gameId: 游戏名 }
//   - `rom_saves[].game_name`：每条 ROM 存档附带游戏名
//   - `h5_indexed_db[db]`：{ version, stores: { name: {keyPath, autoIncrement, indexes, records} } }
//     —— 附带库结构元数据，导入时可忠实重建库
import {
  ROM_DB,
  ROM_STORE,
  ROM_PREFIX,
  NS_PREFIX,
  NS_SEP,
  NS_DB_SEP,
  EXPORT_FORMAT,
  toUint8,
  uint8ToBase64,
  openDB,
  listDatabases,
  serializeValue,
} from './saveCodec.js'

// ---------- 基础工具 ----------

function pad2(n) {
  return String(n).padStart(2, '0')
}

function nowStamp() {
  const d = new Date()
  return `${d.getFullYear()}${pad2(d.getMonth() + 1)}${pad2(d.getDate())}-${pad2(d.getHours())}${pad2(d.getMinutes())}`
}

function safeName(s) {
  return String(s || '')
    .replace(/[\\/:*?"<>|]/g, '_')
    .trim()
    .slice(0, 60)
}

function downloadJSON(obj, filename) {
  const blob = new Blob([JSON.stringify(obj, null, 2)], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// ---------- ROM 存档读取 ----------

// 键 `uogame_save_<gameId>_<rom>` → gameId
function romGameIdOf(key) {
  const rest = key.slice(ROM_PREFIX.length)
  const i = rest.indexOf('_')
  return i < 0 ? rest : rest.slice(0, i)
}

async function readRomEntries() {
  const list = await listDatabases()
  if (list && !list.some((d) => d.name === ROM_DB)) return []
  const db = await openDB(ROM_DB)
  if (!db || !db.objectStoreNames.contains(ROM_STORE)) {
    if (db) db.close()
    return []
  }
  const rows = await new Promise((resolve) => {
    const out = []
    const tx = db.transaction(ROM_STORE, 'readonly')
    const req = tx.objectStore(ROM_STORE).openCursor()
    req.onsuccess = (e) => {
      const cur = e.target.result
      if (!cur) {
        resolve(out)
        return
      }
      const key = cur.key
      if (typeof key === 'string' && key.indexOf(ROM_PREFIX) === 0) {
        const gid = romGameIdOf(key)
        const u8 = toUint8(cur.value)
        out.push({
          key,
          game_id: gid,
          rom: key.slice(ROM_PREFIX.length + gid.length + 1),
          size: u8 ? u8.length : 0,
          base64: u8 ? uint8ToBase64(u8) : null,
        })
      }
      cur.continue()
    }
    req.onerror = () => resolve(out)
  })
  db.close()
  return rows
}

// ---------- H5 存档读取（命名空间前缀） ----------

// 收集主应用视角下带命名空间前缀的 localStorage 项 → { h5g76: { key: value } }
export function collectH5LocalStorage(ns) {
  const out = {}
  try {
    if (!window.localStorage) return out
    for (let i = 0; i < localStorage.length; i += 1) {
      const k = localStorage.key(i)
      if (!k || k.indexOf(NS_PREFIX) !== 0) continue
      const sep = k.indexOf(NS_SEP)
      if (sep <= 0) continue
      const keyNS = k.slice(0, sep)
      if (ns && keyNS !== ns) continue
      if (!out[keyNS]) out[keyNS] = {}
      out[keyNS][k.slice(sep + 1)] = localStorage.getItem(k)
    }
  } catch (e) {
    /* 隐私模式等场景下静默降级 */
  }
  return out
}

// 列出带命名空间的 H5 IndexedDB 库名（h5g<id>::<db>）
async function listH5Databases(ns) {
  const list = await listDatabases()
  if (!list) return []
  return list
    .map((d) => d.name)
    .filter((n) => n.indexOf(NS_PREFIX) === 0 && n.indexOf(NS_DB_SEP) > 0)
    .filter((n) => !ns || n.split(NS_DB_SEP)[0] === ns)
}

// 读取单个 store 的全部记录（IndexedDB 值可能是任意可序列化结构）
function readStoreRecords(store) {
  const hasKeyPath = store.keyPath !== null && store.keyPath !== undefined
  return new Promise((resolve) => {
    const rows = []
    const req = store.openCursor()
    req.onsuccess = (e) => {
      const cur = e.target.result
      if (!cur) {
        resolve(rows)
        return
      }
      const rec = { v: serializeValue(cur.value, 0, []) }
      if (!hasKeyPath) rec.k = serializeValue(cur.key, 0, [])
      rows.push(rec)
      cur.continue()
    }
    req.onerror = () => resolve(rows)
  })
}

// 导出单个库：记录 + 结构元数据（version / keyPath / autoIncrement / indexes）
async function dumpDatabase(name) {
  const db = await openDB(name)
  if (!db) return null
  const out = { version: db.version, stores: {} }
  const storeNames = Array.from(db.objectStoreNames)
  for (let i = 0; i < storeNames.length; i += 1) {
    const sn = storeNames[i]
    let tx
    try {
      tx = db.transaction(sn, 'readonly')
    } catch (e) {
      continue
    }
    const store = tx.objectStore(sn)
    const indexes = Array.from(store.indexNames).map((ixName) => {
      const ix = store.index(ixName)
      return {
        name: ix.name,
        keyPath: ix.keyPath === undefined ? null : ix.keyPath,
        unique: !!ix.unique,
        multiEntry: !!ix.multiEntry,
      }
    })
    const records = await readStoreRecords(store)
    out.stores[sn] = {
      keyPath: store.keyPath === undefined ? null : store.keyPath,
      autoIncrement: !!store.autoIncrement,
      indexes,
      records,
    }
  }
  db.close()
  return out
}

// ---------- 对外 API ----------

// 本机存档概览（供 UI 列表/空态判断，不产生下载）
export async function listSaveSummary() {
  let rom = []
  try {
    rom = await readRomEntries()
  } catch (e) {
    rom = []
  }
  const local = collectH5LocalStorage()
  const dbs = await listH5Databases()

  const map = new Map()
  const ensure = (gid) => {
    if (!map.has(gid)) {
      map.set(gid, { gameId: gid, romCount: 0, romBytes: 0, h5Keys: 0, h5DBs: 0 })
    }
    return map.get(gid)
  }
  rom.forEach((e) => {
    if (!e.game_id) return
    const s = ensure(e.game_id)
    s.romCount += 1
    s.romBytes += e.size || 0
  })
  Object.keys(local).forEach((keyNS) => {
    ensure(keyNS.slice(NS_PREFIX.length)).h5Keys += Object.keys(local[keyNS]).length
  })
  dbs.forEach((n) => {
    ensure(n.split(NS_DB_SEP)[0].slice(NS_PREFIX.length)).h5DBs += 1
  })
  return Array.from(map.values()).filter((s) => s.romCount || s.h5Keys || s.h5DBs)
}

export async function hasAnySave() {
  return (await listSaveSummary()).length > 0
}

// 导出单个游戏的存档
export async function exportSingleGame(gameId, gameName) {
  const gid = String(gameId)
  const ns = `${NS_PREFIX}${gid}`

  const rom = (await readRomEntries())
    .filter((e) => e.game_id === gid)
    .map((e) => ({ ...e, game_name: gameName || '' }))
  const localAll = collectH5LocalStorage(ns)
  const h5Local = localAll[ns] ? { [ns]: localAll[ns] } : {}
  const h5DB = {}
  const dbs = await listH5Databases(ns)
  for (let i = 0; i < dbs.length; i += 1) {
    h5DB[dbs[i]] = await dumpDatabase(dbs[i])
  }

  if (!rom.length && !Object.keys(h5Local).length && !Object.keys(h5DB).length) {
    return { ok: false, reason: 'empty' }
  }

  const payload = {
    format: EXPORT_FORMAT,
    version: 2,
    exported_at: new Date().toISOString(),
    type: 'single',
    game_id: gid,
    game_name: gameName || '',
    games: { [gid]: gameName || '' },
    rom_saves: rom,
    h5_local_storage: h5Local,
    h5_indexed_db: h5DB,
  }
  downloadJSON(payload, `uogame-save-${gid}-${safeName(gameName) || 'game'}.json`)
  return { ok: true, romCount: rom.length }
}

// 导出所有游戏的存档（games：本机游戏列表，用于生成可移植的游戏名表）
export async function exportAllGames(games) {
  const names = {}
  ;(games || []).forEach((g) => {
    if (g && g.id !== undefined) names[String(g.id)] = g.name || ''
  })

  const rom = (await readRomEntries()).map((e) => ({ ...e, game_name: names[e.game_id] || '' }))
  const h5Local = collectH5LocalStorage()
  const dbs = await listH5Databases()
  const h5DB = {}
  for (let i = 0; i < dbs.length; i += 1) {
    h5DB[dbs[i]] = await dumpDatabase(dbs[i])
  }

  if (!rom.length && !Object.keys(h5Local).length && !Object.keys(h5DB).length) {
    return { ok: false, reason: 'empty' }
  }

  // games 名表：覆盖导出中出现的全部 gameId
  const idSet = new Set()
  rom.forEach((e) => e.game_id && idSet.add(String(e.game_id)))
  Object.keys(h5Local).forEach((k) => idSet.add(k.slice(NS_PREFIX.length)))
  dbs.forEach((n) => idSet.add(n.split(NS_DB_SEP)[0].slice(NS_PREFIX.length)))
  const gamesTable = {}
  idSet.forEach((id) => {
    gamesTable[id] = names[id] || ''
  })

  const payload = {
    format: EXPORT_FORMAT,
    version: 2,
    exported_at: new Date().toISOString(),
    type: 'all',
    games: gamesTable,
    rom_saves: rom,
    h5_local_storage: h5Local,
    h5_indexed_db: h5DB,
  }
  downloadJSON(payload, `uogame-saves-all-${nowStamp()}.json`)
  return { ok: true, romCount: rom.length }
}