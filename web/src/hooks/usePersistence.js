// localStorage 键统一常量（与产物保持一致，保证缓存互通）
export const LS = {
  token: 'uogame_token',
  user: 'uogame_user',
  theme: 'uogame_app_theme',
  recentGames: 'uogame_recent_games_list',
  h5KeyMap: 'uogame_h5_key_map',
  savedUsername: 'uogame_saved_username',
  cacheGames: 'cache_games_list',
  cacheCategories: 'cache_categories_list',
  cacheGamesStats: 'cache_games_stats',
  cacheCategoriesStats: 'cache_categories_stats',
  defaultCoverMap: 'uogame_default_cover_map',
  legacyToken: 'token',
}

export function lsGet(key, fallback = null) {
  try {
    const raw = localStorage.getItem(key)
    if (raw == null) return fallback
    return JSON.parse(raw)
  } catch {
    return fallback
  }
}

export function lsSet(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    /* ignore */
  }
}

export function lsRemove(key) {
  try {
    localStorage.removeItem(key)
  } catch {
    /* ignore */
  }
}

// 原始字符串版本（用于 token / username 等纯字符串）
export function lsGetStr(key, fallback = '') {
  try {
    return localStorage.getItem(key) ?? fallback
  } catch {
    return fallback
  }
}

export function lsSetStr(key, value) {
  try {
    localStorage.setItem(key, value)
  } catch {
    /* ignore */
  }
}
