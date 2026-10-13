// 游戏存档导入：解析导出文件 → 按「游戏名」映射到本机游戏 → 写回本机存档。
//
// 映射策略（跨机可移植）：
//   1) 由导出文件的 `games` 名表 / `rom_saves[].game_name` / 顶层 `game_name` 解析出每个导出的游戏名
//   2) 用本机游戏列表（GET /api/games，字段 id/name）按名字匹配：先精确（去空白），再归一（小写 + 折叠空白）
//   3) 匹配不到 → 跳过并计入报告（改由 exportId 直接落位的“同机”导入不在本任务范围）
//
// 写回落点（与导出/注入脚本一一对应）：
//   ROM：IndexedDB `uogameSavesDB/saves`，键重写为 `uogame_save_<本机id>_<rom>`
//   H5 ：localStorage 键重写为 `h5g<本机id>:<key>`（主应用页未 patch，直接原生写入）
//        IndexedDB 库名重写为 `h5g<本机id>::<db>`，按导出元数据建库/建 store/索引后写回记录
//
// 已知边界：目标机须已有同名游戏；重名游戏取首个匹配；Blob/File 值无法还原（跳过计数）；
//          v1「全部」导出无游戏名，无法跨机映射（跳过并提示）。
import {
  ROM_DB,
  ROM_STORE,
  ROM_PREFIX,
  NS_PREFIX,
  NS_SEP,
  NS_DB_SEP,
  EXPORT_FORMAT,
  base64ToUint8,
  openForWrite,
  putRecord,
  deserializeValue,
} from './saveCodec.js'

const ROM_STORE_SPECS = [{ name: ROM_STORE, keyPath: null, autoIncrement: false, indexes: [] }]

// ---------- 解析 ----------

// 解析导出文件文本：接受 v1/v2，校验 format
export function parseExportFile(text) {
  let data
  try {
    data = JSON.parse(text)
  } catch (e) {
    return { ok: false, error: '文件不是合法的 JSON' }
  }
  if (!data || data.format !== EXPORT_FORMAT) {
    return { ok: false, error: '文件格式不匹配（非拾游集存档导出）' }
  }
  const version = Number(data.version) || 1
  if (version > 2) {
    return { ok: false, error: `不支持的文件版本 v${version}` }
  }
  return { ok: true, version, payload: data }
}

// 命名空间 `h5g<id>` → 导出时 gameId；非法返回 null
function idFromNS(ns) {
  if (typeof ns !== 'string' || ns.indexOf(NS_PREFIX) !== 0) return null
  return ns.slice(NS_PREFIX.length)
}

// 导出 gameId → 游戏名（多来源合并）
function exportNameMap(payload) {
  const m = {}
  if (payload.games && typeof payload.games === 'object') {
    Object.keys(payload.games).forEach((k) => {
      if (payload.games[k]) m[String(k)] = String(payload.games[k])
    })
  }
  if (payload.game_id !== undefined && payload.game_name) {
    m[String(payload.game_id)] = String(payload.game_name)
  }
  ;(payload.rom_saves || []).forEach((e) => {
    if (e && e.game_id !== undefined && e.game_name) m[String(e.game_id)] = String(e.game_name)
  })
  return m
}

// 清点导出中出现的全部 gameId 及其数据量
function collectExportGames(payload) {
  const map = new Map()
  const ensure = (id) => {
    const k = String(id)
    if (!map.has(k)) map.set(k, { exportId: k, romCount: 0, h5Keys: 0, h5DBs: 0 })
    return map.get(k)
  }
  ;(payload.rom_saves || []).forEach((e) => {
    if (e && e.game_id !== undefined) ensure(e.game_id).romCount += 1
  })
  const local = payload.h5_local_storage || {}
  Object.keys(local).forEach((ns) => {
    const id = idFromNS(ns)
    if (id !== null) ensure(id).h5Keys += Object.keys(local[ns] || {}).length
  })
  Object.keys(payload.h5_indexed_db || {}).forEach((dbName) => {
    const id = idFromNS(String(dbName).split(NS_DB_SEP)[0])
    if (id !== null) ensure(id).h5DBs += 1
  })
  return Array.from(map.values())
}

// ---------- 按名映射 ----------

function normName(s) {
  return String(s == null ? '' : s)
    .replace(/\s+/g, ' ')
    .trim()
    .toLowerCase()
}

// 本机 名字 → gameId 索引（精确 + 归一）
function localNameIndex(games) {
  const exact = new Map()
  const norm = new Map()
  ;(games || []).forEach((g) => {
    if (!g || g.id === undefined) return
    const name = String(g.name == null ? '' : g.name).trim()
    if (!name) return
    const id = String(g.id)
    if (!exact.has(name)) exact.set(name, id)
    const n = normName(name)
    if (n && !norm.has(n)) norm.set(n, id)
  })
  return { exact, norm }
}

function matchLocal(name, index) {
  if (!name) return null
  const trimmed = String(name).trim()
  if (index.exact.has(trimmed)) return index.exact.get(trimmed)
  const n = normName(trimmed)
  if (n && index.norm.has(n)) return index.norm.get(n)
  return null
}

// 生成导入计划（不产生写入）：matched 可导入；skipped 不可导入（附原因）
export function buildImportPlan(payload, games) {
  const names = exportNameMap(payload)
  const index = localNameIndex(games)
  const matched = []
  const skipped = []
  collectExportGames(payload).forEach((it) => {
    const gameName = names[it.exportId] || ''
    const localId = matchLocal(gameName, index)
    const row = { ...it, gameName, localId }
    if (localId) matched.push(row)
    else skipped.push({ ...row, reason: gameName ? 'no-name-match' : 'no-name' })
  })
  return { matched, skipped, version: Number(payload.version) || 1, type: payload.type || 'all' }
}

// ---------- 写回 ----------

async function importRoms(payload, idMap, report) {
  const entries = (payload.rom_saves || []).filter((e) => e && e.game_id !== undefined && idMap[String(e.game_id)])
  if (!entries.length) return
  const db = await openForWrite(ROM_DB, 1, ROM_STORE_SPECS)
  if (!db) {
    report.errors.push('无法打开 ROM 存档库')
    report.rom.skipped += entries.length
    return
  }
  for (let i = 0; i < entries.length; i += 1) {
    const e = entries[i]
    try {
      const key = `${ROM_PREFIX}${idMap[String(e.game_id)]}_${e.rom}`
      const bytes = base64ToUint8(e.base64)
      const ok = await putRecord(db, ROM_STORE, bytes, key)
      if (ok) report.rom.written += 1
      else report.rom.skipped += 1
    } catch (err) {
      report.rom.skipped += 1
    }
  }
  db.close()
}

function importH5Local(payload, idMap, report) {
  const all = payload.h5_local_storage || {}
  Object.keys(all).forEach((ns) => {
    const kv = all[ns] || {}
    const exportId = idFromNS(ns)
    const localId = exportId !== null ? idMap[exportId] : null
    if (!localId) {
      report.h5Local.skipped += Object.keys(kv).length
      return
    }
    Object.keys(kv).forEach((key) => {
      try {
        localStorage.setItem(`${NS_PREFIX}${localId}${NS_SEP}${key}`, kv[key])
        report.h5Local.written += 1
      } catch (e) {
        report.h5Local.skipped += 1
      }
    })
  })
}

// v2 结构 {version, stores:{name:{keyPath,autoIncrement,indexes,records}}}；v1 旧结构 {store:[records]}
function normalizeDbDump(raw) {
  if (raw && typeof raw === 'object' && raw.stores && typeof raw.stores === 'object') {
    const stores = {}
    Object.keys(raw.stores).forEach((sn) => {
      const s = raw.stores[sn] || {}
      stores[sn] = {
        keyPath: s.keyPath === undefined ? null : s.keyPath,
        autoIncrement: !!s.autoIncrement,
        indexes: Array.isArray(s.indexes) ? s.indexes : [],
        records: Array.isArray(s.records) ? s.records : [],
      }
    })
    return { version: raw.version || 1, stores }
  }
  const stores = {}
  Object.keys(raw || {}).forEach((sn) => {
    stores[sn] = { keyPath: null, autoIncrement: false, indexes: [], records: Array.isArray(raw[sn]) ? raw[sn] : [] }
  })
  return { version: 1, stores }
}

function countRecords(dump) {
  return Object.keys(dump.stores).reduce((n, sn) => n + (dump.stores[sn].records || []).length, 0)
}

async function importH5DBs(payload, idMap, report) {
  const all = payload.h5_indexed_db || {}
  const dbNames = Object.keys(all)
  for (let i = 0; i < dbNames.length; i += 1) {
    const dbName = dbNames[i]
    const ns = String(dbName).split(NS_DB_SEP)[0]
    const suffix = String(dbName).slice(ns.length + NS_DB_SEP.length)
    const exportId = idFromNS(ns)
    const localId = exportId !== null ? idMap[exportId] : null
    const dump = normalizeDbDump(all[dbName])
    if (!localId) {
      report.h5DB.skipped += countRecords(dump)
      continue
    }
    const specs = Object.keys(dump.stores).map((sn) => ({
      name: sn,
      keyPath: dump.stores[sn].keyPath,
      autoIncrement: dump.stores[sn].autoIncrement,
      indexes: dump.stores[sn].indexes,
    }))
    const target = `${NS_PREFIX}${localId}${NS_DB_SEP}${suffix}`
    const db = await openForWrite(target, dump.version, specs)
    if (!db) {
      report.errors.push(`无法创建库 ${target}`)
      report.h5DB.skipped += countRecords(dump)
      continue
    }
    const storeNames = Object.keys(dump.stores)
    for (let s = 0; s < storeNames.length; s += 1) {
      const sn = storeNames[s]
      const st = dump.stores[sn]
      if (!db.objectStoreNames.contains(sn)) {
        report.h5DB.skipped += (st.records || []).length
        continue
      }
      const hasKeyPath = st.keyPath !== null && st.keyPath !== undefined
      for (let r = 0; r < st.records.length; r += 1) {
        const rec = st.records[r] || {}
        const vr = deserializeValue(rec.v)
        if (!vr.ok) {
          report.h5DB.skipped += 1
          continue
        }
        let key
        if (!hasKeyPath) {
          const kr = deserializeValue(rec.k)
          if (!kr.ok) {
            report.h5DB.skipped += 1
            continue
          }
          key = kr.value
        }
        const ok = await putRecord(db, sn, vr.value, key)
        if (ok) report.h5DB.written += 1
        else report.h5DB.skipped += 1
      }
    }
    db.close()
  }
}

// 执行导入：返回结果报告
export async function importSaves(payload, games) {
  const plan = buildImportPlan(payload, games)
  const idMap = {}
  plan.matched.forEach((m) => {
    idMap[m.exportId] = m.localId
  })
  const report = {
    version: plan.version,
    type: plan.type,
    matchedGames: plan.matched.map((m) => ({ exportId: m.exportId, gameName: m.gameName, localId: m.localId })),
    skippedGames: plan.skipped,
    rom: { written: 0, skipped: 0 },
    h5Local: { written: 0, skipped: 0 },
    h5DB: { written: 0, skipped: 0 },
    errors: [],
  }
  await importRoms(payload, idMap, report)
  importH5Local(payload, idMap, report)
  await importH5DBs(payload, idMap, report)
  return report
}