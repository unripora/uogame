// 格式化工具（对齐产物中的时长/字节格式化逻辑）

// 字节格式化：B / KB / MB / GB
export function formatBytes(bytes) {
  const n = Number(bytes) || 0
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

// 游玩时长格式化：秒 → "X时Y分" / "X时" / "Y分"
export function formatPlayTime(seconds) {
  const s = Number(seconds) || 0
  const totalMinutes = Math.floor(s / 60)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours > 0 && minutes > 0) return `${hours}时${minutes}分`
  if (hours > 0) return `${hours}时`
  return `${minutes}分`
}

// 时间戳（秒）→ 本地日期时间字符串
export function formatDateTime(ts) {
  const n = Number(ts) || 0
  if (!n) return ''
  const d = new Date(n * 1000)
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(
    d.getHours()
  )}:${p(d.getMinutes())}`
}

// 生成导出文件名时间戳：YYYYMMDD_HHMMSS
export function timestampForFile(date = new Date()) {
  const p = (x) => String(x).padStart(2, '0')
  return `${date.getFullYear()}${p(date.getMonth() + 1)}${p(
    date.getDate()
  )}_${p(date.getHours())}${p(date.getMinutes())}${p(date.getSeconds())}`
}

// 文件名安全化（去除非法字符）
export function safeFileName(name) {
  return String(name || 'game').replace(/[/\\:*?"<>|]/g, '_')
}
