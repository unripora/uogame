// 游戏别名（曾用名）工具
// 后端以 JSON 数组字符串存储于 game.aliases，如 '["玩家国度","开店我最牛"]'
// 本模块统一负责解析，避免各处重复判断。

// 解析别名字段为字符串数组；空/非法时返回空数组
export function parseAliases(raw) {
  if (!raw) return []
  if (Array.isArray(raw)) return raw.filter((x) => typeof x === 'string' && x.trim() !== '')
  if (typeof raw !== 'string') return []
  try {
    const arr = JSON.parse(raw)
    if (Array.isArray(arr)) {
      return arr.filter((x) => typeof x === 'string' && x.trim() !== '')
    }
  } catch {
    // 非 JSON：容错为按顿号/逗号分隔
    return raw
      .split(/[、,，]/)
      .map((s) => s.trim())
      .filter(Boolean)
  }
  return []
}
