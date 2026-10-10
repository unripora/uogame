// 主题定义（从产物反解：5 套主题）
// id 与 <html data-theme="..."> 及 CSS 变量（styles/index.css）对应
export const THEMES = [
  {
    id: 'light',
    name: '极简明朗',
    subtitle: '默认浅色',
    desc: '经典湛蓝高光与清爽白底，简约护眼',
  },
  {
    id: 'dark',
    name: '暗夜深邃',
    subtitle: '经典深色',
    desc: '黑曜石纯黑与极客电蓝，深邃沉浸且省电',
  },
  {
    id: 'cyberpunk',
    name: '赛博霓虹',
    subtitle: '游戏厅限定',
    desc: '街机电玩城洋红与青色霓虹流光，临场感拉满',
  },
  {
    id: 'arcade',
    name: '复古街机',
    subtitle: '经典投币机',
    desc: '80/90年代CRT显像管炭黑与暖金琥珀微光',
  },
  {
    id: 'aurora',
    name: '暮色极光',
    subtitle: '梦幻星河',
    desc: '深空星系黑与翡翠青、星云紫双色幻彩流光',
  },
]

export const THEME_KEY = 'uogame_app_theme'
export const DEFAULT_THEME = 'light'

export function isValidTheme(id) {
  return THEMES.some((t) => t.id === id)
}

export function applyTheme(id) {
  const theme = isValidTheme(id) ? id : DEFAULT_THEME
  document.documentElement.setAttribute('data-theme', theme)
  return theme
}

export function loadStoredTheme() {
  try {
    const saved = localStorage.getItem(THEME_KEY)
    return isValidTheme(saved) ? saved : DEFAULT_THEME
  } catch {
    return DEFAULT_THEME
  }
}

export function storeTheme(id) {
  try {
    localStorage.setItem(THEME_KEY, id)
  } catch {
    /* ignore */
  }
}
