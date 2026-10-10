// 统一 API 端点常量（从产物反解得到的 50 个调用路径）
export const API = {
  // 认证
  authLogin: '/api/auth/login',
  authRegister: '/api/auth/register',
  authLogout: '/api/auth/logout',
  authVerify: '/api/auth/verify',

  // 公开
  publicSettings: '/api/public/settings',
  publicVersion: '/api/public/version',
  hardware: '/api/hardware',

  // 游戏
  games: '/api/games',
  // 分页查询（管理面板等大数据量场景）：page 从 1 起，q 为关键词（匹配主名/别名）
  gamesPaged: ({ page = 1, pageSize = 50, q = '' } = {}) => {
    const p = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (q) p.set('q', q)
    return `/api/games?${p.toString()}`
  },
  tags: '/api/tags',
  gamesStats: '/api/games/stats',
  gamesChanges: '/api/games/changes',
  gamesImport: '/api/games/import',
  gamesSource: '/api/games/source',
  gamesCheckDuplicate: '/api/games/check-duplicate',
  gamesDedupe: '/api/games/dedupe',
  gamesBatchExport: '/api/games/batch-export',
  gameConfig: '/api/games/config.json',
  game: (id) => `/api/games/${id}`,
  gameCover: (id) => `/api/games/${id}/cover`,
  gameExport: (id) => `/api/games/${id}/export`,
  gameRoms: (id) => `/api/games/${id}/roms`,
  gameImages: (id) => `/api/games/${id}/images`,
  gameScrape: (id) => `/api/games/${id}/scrape`,

  // 游戏评分（一人一票，可修改/取消）
  gameRating: (id) => `/api/games/${id}/rating`,
  myRatings: '/api/ratings/mine',

  // 分类
  categories: '/api/categories',
  categoriesStats: '/api/categories/stats',
  category: (id) => `/api/categories/${id}`,

  // 用户
  userProfile: '/api/user/profile',
  userSwitchIdentity: '/api/user/switch-identity',
  userSave: '/api/user/save',
  userPlaytime: '/api/user/playtime',
  userBackups: '/api/user/backups',
  userBackup: (id) => `/api/user/backups/${id}`,
  userBackupRestore: (id) => `/api/user/backups/${id}/restore`,
  userBackupExport: (id) => `/api/user/backups/${id}/export`,
  userBackupsImport: '/api/user/backups/import',

  // 聊天
  chatHistory: '/api/chat/history',
  wsChat: '/api/ws/chat',

  // 多人联机房间
  rooms: '/api/rooms',
  room: (id) => `/api/rooms/${id}`,
  wsRoom: (id) => `/api/ws/room/${id}`,

  // 排行榜
  leaderboardPlaytime: '/api/leaderboard/playtime',
  leaderboardHotGames: '/api/leaderboard/hotgames',
  leaderboardsReset: '/api/leaderboards/reset',

  // 管理员
  adminUsers: '/api/admin/users',
  adminUser: (id) => `/api/admin/users/${id}`,
  adminUserPromote: (id) => `/api/admin/users/${id}/promote`,
  adminManagers: '/api/admin/managers',
  adminManager: (id) => `/api/admin/managers/${id}`,
  adminBinds: '/api/admin/binds',
  adminBind: (id) => `/api/admin/binds/${id}`,
  adminSettings: '/api/admin/settings',
  adminChat: '/api/admin/chat',
  adminAccessiblePaths: '/api/admin/accessible-paths',
  adminBrowsePath: '/api/admin/accessible-paths/browse',
  adminLeaderboardsReset: '/api/admin/leaderboards/reset',

  // 白名单分类（新增功能）
  adminWhitelist: '/api/admin/whitelist',
  gamesClassify: '/api/games/classify',
  gamesClassifyUndo: '/api/games/classify/undo',
}

// 游玩 iframe 路径契约（保持与后端一致）
export const PLAY = {
  h5: (id) => `/play/${id}/index.html`,
  emulator: (id, rom) => `/play/${id}/${rom}`,
  emulatorPage: '/emulator/emulatorjs.html',
  rufflePage: '/emulator/ruffle.html',
}
