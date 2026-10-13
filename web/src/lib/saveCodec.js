// 存档编解码与 IndexedDB 通用工具：导出(saveExport)与导入(saveImport)共用。
//
// 键位/库名约定（与 emulatorjs.html 的 SaveDB、savens.go 注入脚本保持一致）：
//   ROM 存档：IndexedDB `uogameSavesDB` / store `saves`（无 keyPath，键为字符串）
//             键 `uogame_save_<gameId>_<romName>`，值 Uint8Array
//   H5 存档： localStorage 键 `h5g<gameId>:<key>`
//             IndexedDB   库名 `h5g<gameId>::<db>`

export const ROM_DB = 'uogameSavesDB'
export const ROM_STORE = 'saves'
export const ROM_PREFIX = 'uogame_save_'
export const NS_PREFIX = 'h5g'
export const NS_SEP = ':'
export const NS_DB_SEP = '::'
export const EXPORT_FORMAT = 'uogame-save-export'

// ---------- 字节 / base64 ----------

export function toUint8(v) {
  if (v instanceof Uint8Array) return v
  if (v instanceof ArrayBuffer) return new Uint8Array(v)
  if (v && ArrayBuffer.isView(v)) return new Uint8Array(v.buffer, v.byteOffset, v.byteLength)
  return null
}

// Uint8Array → base64（分块，避免超长参数栈溢出）
export function uint8ToBase64(bytes) {
  const u8 = toUint8(bytes)
  if (u8 === null) return ''
  let bin = ''
  const CHUNK = 0x8000
  for (let i = 0; i < u8.length; i += CHUNK) {
    bin += String.fromCharCode.apply(null, u8.subarray(i, i + CHUNK))
  }
  return btoa(bin)
}

export function base64ToUint8(b64) {
  if (!b64) return new Uint8Array(0)
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i += 1) out[i] = bin.charCodeAt(i)
  return out
}

// ---------- IndexedDB 访问 ----------

// 打开库。opts.version 缺省时不改变版本；opts.upgrade 仅在升级事务中调用。
// 失败（含 blocked）一律 resolve(null)，由调用方降级处理。
export function openDB(name, opts) {
  const o = opts || {}
  return new Promise((resolve) => {
    if (!window.indexedDB) {
      resolve(null)
      return
    }
    let req
    try {
      req = o.version ? indexedDB.open(name, o.version) : indexedDB.open(name)
    } catch (e) {
      resolve(null)
      return
    }
    // 无 upgrade 回调时不创建任何 store（保持“库不存在即空”的语义）
    req.onupgradeneeded = () => {
      if (o.upgrade) {
        try {
          o.upgrade(req.result, req.transaction)
        } catch (e) {
          /* 升级异常交由 open 失败（onerror/onblocked）兜底 */
        }
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => resolve(null)
    req.onblocked = () => resolve(null)
  })
}

// 列出全部库（含版本）；浏览器不支持 databases() 时返回 null 表示“未知”
export async function listDatabases() {
  if (!window.indexedDB || typeof indexedDB.databases !== 'function') return null
  try {
    const list = await indexedDB.databases()
    return (list || [])
      .filter((d) => d && typeof d.name === 'string')
      .map((d) => ({ name: d.name, version: d.version || 0 }))
  } catch (e) {
    return null
  }
}

async function findDatabase(name) {
  const list = await listDatabases()
  if (!list) return undefined // 未知
  const hit = list.find((d) => d.name === name)
  return hit || null // null 明确不存在
}

// 在升级事务内创建缺失的 store 与索引
function applySpecs(db, tx, specs) {
  ;(specs || []).forEach((s) => {
    if (!s || !s.name) return
    let store
    if (!db.objectStoreNames.contains(s.name)) {
      const opt = {}
      if (s.keyPath !== undefined && s.keyPath !== null) opt.keyPath = s.keyPath
      if (s.autoIncrement) opt.autoIncrement = true
      store = db.createObjectStore(s.name, opt)
    } else {
      store = tx.objectStore(s.name)
    }
    ;(s.indexes || []).forEach((ix) => {
      if (!ix || !ix.name || ix.keyPath === undefined || ix.keyPath === null) return
      if (store.indexNames.contains(ix.name)) return
      try {
        store.createIndex(ix.name, ix.keyPath, { unique: !!ix.unique, multiEntry: !!ix.multiEntry })
      } catch (e) {
        /* 索引创建失败不影响数据写入 */
      }
    })
  })
}

// 打开（必要时创建/升级）库，确保 store 存在；已存在则保持版本不变（不阻塞既有连接）
export async function openForWrite(dbName, version, storeSpecs) {
  const specs = storeSpecs || []
  const info = await findDatabase(dbName)
  if (info === null) {
    // 明确不存在：按导出（或默认）版本创建
    return openDB(dbName, { version: version || 1, upgrade: (db, tx) => applySpecs(db, tx, specs) })
  }
  const db = await openDB(dbName)
  if (!db) return null
  const missing = specs.some((s) => s && s.name && !db.objectStoreNames.contains(s.name))
  if (!missing) return db // 结构齐备：直接用，避免无谓升级
  const cur = db.version
  db.close()
  return openDB(dbName, { version: cur + 1, upgrade: (d, tx) => applySpecs(d, tx, specs) })
}

// 写入单条记录（key 为 undefined 时使用 keyPath/inline key）
export function putRecord(db, storeName, value, key) {
  return new Promise((resolve) => {
    let tx
    try {
      tx = db.transaction(storeName, 'readwrite')
    } catch (e) {
      resolve(false)
      return
    }
    tx.oncomplete = () => resolve(true)
    tx.onerror = () => resolve(false)
    tx.onabort = () => resolve(false)
    try {
      const store = tx.objectStore(storeName)
      if (key === undefined) store.put(value)
      else store.put(value, key)
    } catch (e) {
      resolve(false)
    }
  })
}

// ---------- 值序列化 / 反序列化 ----------

// 通用值序列化（IndexedDB 可存 Blob/Map/Set/ArrayBuffer 等，JSON 无法直接表达）
export function serializeValue(v, depth, seen) {
  if (v === undefined) return { __type: 'undefined' }
  if (v === null) return null
  const t = typeof v
  if (t === 'string' || t === 'number' || t === 'boolean') return v
  if (t === 'bigint') return { __type: 'bigint', value: String(v) }
  if (t === 'function') return { __type: 'function' }
  if (t === 'symbol') return { __type: 'symbol', value: String(v) }
  if (depth > 16) return { __type: 'depth-limit' }
  if (seen.indexOf(v) >= 0) return { __type: 'circular' }
  if (v instanceof Date) return { __type: 'date', value: v.toISOString() }
  if (v instanceof ArrayBuffer) return { __type: 'bytes', base64: uint8ToBase64(new Uint8Array(v)) }
  if (ArrayBuffer.isView(v)) {
    const u8 = new Uint8Array(v.buffer, v.byteOffset, v.byteLength)
    return {
      __type: 'typed-array',
      name: (v.constructor && v.constructor.name) || 'TypedArray',
      base64: uint8ToBase64(u8),
    }
  }
  if (typeof Blob !== 'undefined' && v instanceof Blob) {
    return { __type: 'blob', mime: v.type || '', size: v.size }
  }
  if (typeof File !== 'undefined' && v instanceof File) {
    return { __type: 'file', name: v.name, mime: v.type || '', size: v.size }
  }
  if (v instanceof RegExp) return { __type: 'regexp', value: String(v) }
  const nextSeen = seen.concat([v])
  if (Array.isArray(v)) return v.map((item) => serializeValue(item, depth + 1, nextSeen))
  if (v instanceof Map) {
    const entries = []
    v.forEach((val, key) => {
      entries.push([serializeValue(key, depth + 1, nextSeen), serializeValue(val, depth + 1, nextSeen)])
    })
    return { __type: 'map', entries }
  }
  if (v instanceof Set) {
    const values = []
    v.forEach((val) => values.push(serializeValue(val, depth + 1, nextSeen)))
    return { __type: 'set', values }
  }
  const obj = {}
  Object.keys(v).forEach((k) => {
    obj[k] = serializeValue(v[k], depth + 1, nextSeen)
  })
  return obj
}

const TYPED_ARRAYS = {
  Int8Array,
  Uint8Array,
  Uint8ClampedArray,
  Int16Array,
  Uint16Array,
  Int32Array,
  Uint32Array,
  Float32Array,
  Float64Array,
  BigInt64Array: typeof BigInt64Array !== 'undefined' ? BigInt64Array : null,
  BigUint64Array: typeof BigUint64Array !== 'undefined' ? BigUint64Array : null,
}

function typedArrayFrom(name, b64) {
  const u8 = base64ToUint8(b64)
  if (name === 'DataView') return { ok: true, value: new DataView(u8.buffer) }
  const Ctor = TYPED_ARRAYS[name]
  if (!Ctor || u8.byteLength % Ctor.BYTES_PER_ELEMENT !== 0) return { ok: true, value: u8 }
  return { ok: true, value: new Ctor(u8.buffer) }
}

function parseRegExp(s) {
  const str = String(s)
  const m = /^\/(.*)\/([a-z]*)$/.exec(str)
  if (!m) return new RegExp(str)
  try {
    return new RegExp(m[1], m[2])
  } catch (e) {
    return new RegExp(m[1])
  }
}

// 反序列化：返回 { ok, value }；ok=false 表示该值本机无法还原（Blob/File/函数/深度截断等）
export function deserializeValue(env) {
  if (env === null || env === undefined) return { ok: true, value: env }
  const t = typeof env
  if (t === 'string' || t === 'number' || t === 'boolean') return { ok: true, value: env }
  if (t !== 'object') return { ok: true, value: env }
  if (Array.isArray(env)) {
    const out = []
    for (let i = 0; i < env.length; i += 1) {
      const r = deserializeValue(env[i])
      if (!r.ok) return { ok: false }
      out.push(r.value)
    }
    return { ok: true, value: out }
  }
  const ty = env.__type
  if (!ty) {
    const out = {}
    const keys = Object.keys(env)
    for (let i = 0; i < keys.length; i += 1) {
      const r = deserializeValue(env[keys[i]])
      if (!r.ok) return { ok: false }
      out[keys[i]] = r.value
    }
    return { ok: true, value: out }
  }
  switch (ty) {
    case 'undefined':
      return { ok: true, value: undefined }
    case 'bigint':
      return { ok: true, value: BigInt(env.value) }
    case 'date':
      return { ok: true, value: new Date(env.value) }
    case 'bytes':
      return { ok: true, value: base64ToUint8(env.base64).buffer }
    case 'typed-array':
      return typedArrayFrom(env.name, env.base64)
    case 'regexp':
      return { ok: true, value: parseRegExp(env.value) }
    case 'map': {
      const m = new Map()
      const entries = env.entries || []
      for (let i = 0; i < entries.length; i += 1) {
        const kr = deserializeValue(entries[i][0])
        const vr = deserializeValue(entries[i][1])
        if (!kr.ok || !vr.ok) return { ok: false }
        m.set(kr.value, vr.value)
      }
      return { ok: true, value: m }
    }
    case 'set': {
      const s = new Set()
      const values = env.values || []
      for (let i = 0; i < values.length; i += 1) {
        const r = deserializeValue(values[i])
        if (!r.ok) return { ok: false }
        s.add(r.value)
      }
      return { ok: true, value: s }
    }
    // 以下类型无法在本机还原，交由调用方跳过
    case 'blob':
    case 'file':
    case 'function':
    case 'symbol':
    case 'depth-limit':
    case 'circular':
    default:
      return { ok: false }
  }
}