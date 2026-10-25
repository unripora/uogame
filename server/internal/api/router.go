package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/api/handlers"
	"uogame-go/internal/api/middleware"
	"uogame-go/internal/config"
	"uogame-go/internal/models"
)

// SetupRouter 构建并返回 Gin 引擎，注册全部路由与静态资源
func SetupRouter(cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	// 跨域支持（游戏源模式）：白名单由 CORS_ORIGINS 配置，未配置则不开放跨域
	r.Use(middleware.CORSMiddleware(cfg))

	// 注入配置到 handler 包
	handlers.Init(cfg)

	api := r.Group("/api")

	// === 公开路由（无需登录） ===
	api.POST("/auth/login", handlers.Login)
	api.POST("/auth/register", handlers.Register)
	api.POST("/auth/logout", handlers.Logout)
	api.GET("/public/settings", handlers.GetPublicSettings)
	// 源描述发现端点（公开，供前端自动发现源配置）
	r.GET("/.well-known/uogame-source.json", handlers.SourceWellKnown)
	api.GET("/public/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "1.0.8"})
	})
	// PWA manifest（index.html 中引用）
	api.GET("/public/pwa-profile", handlers.PwaProfile)

	// === 需登录路由 ===
	auth := api.Group("")
	auth.Use(middleware.AuthMiddleware(cfg))
	{
		auth.GET("/auth/verify", handlers.Verify)

		// 游戏（读取/游玩：登录即可）
		auth.GET("/games", handlers.ListGames)
		auth.GET("/games/stats", handlers.GetGamesStats)
		auth.GET("/games/changes", handlers.GetGamesChanges)
		auth.GET("/tags", handlers.ListTags)
		auth.GET("/games/config.json", handlers.GameConfigHandler)
		auth.GET("/games/:id", handlers.GetGame)
		auth.GET("/games/:id/cover", handlers.GetGameCover)
		auth.GET("/games/:id/roms", handlers.ListGameRoms)
		auth.GET("/games/:id/ons-manifest", handlers.GetOnsManifest)
		auth.GET("/games/:id/images", handlers.GetGameImages)
		auth.GET("/games/:id/play/*file", handlers.ServeGameFile)

		// 游戏源协议（源模式）：清单 / 搜索 / 详情（登录即可，供源拉取元数据）
		auth.GET("/source/manifest", handlers.SourceManifest)
		auth.GET("/source/search", handlers.SourceSearch)
		auth.GET("/source/game/:id", handlers.SourceGameDetail)

		// 游戏评分（登录即可；一人一票，可修改/取消）
		auth.POST("/games/:id/rating", handlers.RateGame)
		auth.DELETE("/games/:id/rating", handlers.UnrateGame)
		auth.GET("/ratings/mine", handlers.ListMyRatings)

		// 游戏管理（写操作：需 game.manage 权限点）
		gameManage := auth.Group("/games")
		gameManage.Use(middleware.RequirePermission(models.PermGameManage))
		{
			gameManage.POST("", handlers.CreateGame)
			gameManage.POST("/import", handlers.ImportGame)
			gameManage.POST("/source", handlers.ImportSourceFile)
			gameManage.POST("/check-duplicate", handlers.CheckDuplicate)
			gameManage.POST("/dedupe", handlers.DedupeGames)
			gameManage.POST("/batch-export", handlers.BatchExportGames)
			gameManage.POST("/batch-category", handlers.BatchUpdateGameCategory)
			gameManage.POST("/batch-delete", handlers.BatchDeleteGames)
			gameManage.POST("/classify", handlers.ClassifyGames)
			gameManage.POST("/classify/undo", handlers.UndoClassify)
			gameManage.PUT("/:id", handlers.UpdateGame)
			gameManage.DELETE("/:id", handlers.DeleteGame)
			gameManage.GET("/:id/export", handlers.ExportGame)
			gameManage.POST("/:id/roms", handlers.UploadGameRoms)
			gameManage.POST("/:id/scrape", handlers.ScrapeGame)
		}

		// 分类（读取：登录即可）
		auth.GET("/categories", handlers.ListCategories)
		auth.GET("/categories/stats", handlers.GetCategoriesStats)

		// 分类管理（写操作：需 category.manage 权限点）
		catManage := auth.Group("/categories")
		catManage.Use(middleware.RequirePermission(models.PermCategoryManage))
		{
			catManage.POST("", handlers.CreateCategory)
			catManage.PUT("/:id", handlers.UpdateCategory)
			catManage.DELETE("/:id", handlers.DeleteCategory)
		}

		// 用户
		auth.GET("/user/profile", handlers.GetUserProfile)
		auth.PUT("/user/profile", handlers.UpdateUserProfile)
		auth.POST("/user/switch-identity", handlers.SwitchIdentity)
		auth.GET("/user/save", handlers.GetUserSave)
		auth.PUT("/user/save", handlers.UpdateUserSave)
		auth.POST("/user/playtime", handlers.RecordPlayTime)
		auth.GET("/user/backups", handlers.ListSaveBackups)
		auth.POST("/user/backups", handlers.CreateSaveBackup)
		auth.PUT("/user/backups/:id", handlers.UpdateSaveBackupTitle)
		auth.DELETE("/user/backups/:id", handlers.DeleteSaveBackup)
		auth.POST("/user/backups/:id/restore", handlers.RestoreSaveBackup)
		auth.GET("/user/backups/:id/export", handlers.ExportSaveBackup)
		auth.POST("/user/backups/import", handlers.ImportSaveBackup)

		// 聊天
		auth.GET("/chat/history", handlers.GetChatHistory)
		auth.DELETE("/chat/history", handlers.ClearChatHistory)
		auth.GET("/ws/chat", handlers.WsChat)

		// 多人联机房间（登录即可：创建/列表/详情/关闭）
		auth.GET("/rooms", handlers.ListRooms)
		auth.POST("/rooms", handlers.CreateRoom)
		auth.GET("/rooms/:id", handlers.GetRoomDetail)
		auth.DELETE("/rooms/:id", handlers.DeleteRoom)
		auth.GET("/ws/room/:id", handlers.WsRoom)

		// 排行榜
		auth.GET("/leaderboard/playtime", handlers.GetPlayTimeLeaderboard)
		auth.GET("/leaderboard/hotgames", handlers.GetHotGamesLeaderboard)
		auth.POST("/leaderboards/reset", handlers.ResetLeaderboards)

		// 管理员（按权限点细分；super_admin 全放行，admin 按勾选权限点）
		// 用户管理
		adminUsers := auth.Group("/admin")
		adminUsers.Use(middleware.RequirePermission(models.PermUserManage))
		{
			adminUsers.GET("/users", handlers.ListUsers)
			// 玩家提权为管理员（自动建立人员绑定）
			adminUsers.POST("/users/:id/promote", handlers.PromotePlayer)
			// 管理员账号管理（独立体系）
			adminUsers.GET("/managers", handlers.ListManagers)
			adminUsers.POST("/managers", handlers.CreateManager)
			adminUsers.PUT("/managers/:id", handlers.UpdateManager)
			adminUsers.DELETE("/managers/:id", handlers.DeleteManager)
			// 玩家↔管理员 人员绑定
			adminUsers.GET("/binds", handlers.ListBinds)
			adminUsers.POST("/binds", handlers.CreateBind)
			adminUsers.DELETE("/binds/:id", handlers.DeleteBind)
		}
		// 系统设置 + 可访问路径
		adminSettings := auth.Group("/admin")
		adminSettings.Use(middleware.RequirePermission(models.PermSettingManage))
		{
			adminSettings.GET("/settings", handlers.GetSettings)
			adminSettings.PUT("/settings", handlers.UpdateSetting)
			adminSettings.GET("/accessible-paths", handlers.GetAccessiblePaths)
			adminSettings.GET("/accessible-paths/browse", handlers.BrowseAccessiblePath)
		}
		// 聊天管理
		adminChat := auth.Group("/admin")
		adminChat.Use(middleware.RequirePermission(models.PermChatManage))
		{
			adminChat.DELETE("/chat", handlers.AdminClearChat)
		}
		// 榜单管理
		adminLeaderboard := auth.Group("/admin")
		adminLeaderboard.Use(middleware.RequirePermission(models.PermLeaderboardManage))
		{
			adminLeaderboard.POST("/leaderboards/reset", handlers.ResetLeaderboards)
		}
		// 分类管理（白名单配置）
		adminCategory := auth.Group("/admin")
		adminCategory.Use(middleware.RequirePermission(models.PermCategoryManage))
		{
			adminCategory.GET("/whitelist", handlers.GetWhitelist)
			adminCategory.PUT("/whitelist", handlers.UpdateWhitelist)
		}
	}

	// 硬件信息（公开）
	api.GET("/hardware", handlers.GetHardwareInfo)

	// === 根路径游戏资源路由（前端 iframe 契约） ===
	// 前端打包 JS 中 iframe/game 请求使用根路径：/play/{id}/index.html?token=...
	// 因此必须在根注册（而非 /api 前缀下），并复用鉴权中间件（支持 ?token= 通道）
	// 鉴权失败统一返回 404（HiddenAuthMiddleware），避免通过状态码探测游戏/文件是否存在
	play := r.Group("/play")
	play.Use(middleware.HiddenAuthMiddleware(cfg))
	{
		play.GET("/:id/*file", handlers.ServeGameFile)
	}

	// 游戏存档命名空间代理脚本（H5 页面注入；公开静态资源，需在 NoRoute 之前注册）
	r.GET("/assets/savens-ns.js", handlers.ServeSaveNamespaceScript)

	// 静态前端资源（复用原有 ui 产物）
	// 前端 index.html 使用绝对根路径引用 /assets、/emulator、/icon_64.png 等，
	// 因此统一在 NoRoute 中处理：命中真实文件则返回文件，否则回退到 index.html（SPA）。
	uiDir := resolveUIDir()
	r.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path

		// API 前缀未匹配 → JSON 兜底
		if strings.HasPrefix(reqPath, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
			return
		}

		if uiDir == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "UI not found"})
			return
		}

		// 尝试返回真实静态文件（防止路径穿越）
		clean := filepath.Clean("/" + reqPath)
		full := filepath.Join(uiDir, clean)
		if strings.HasPrefix(full, uiDir) {
			if info, err := os.Stat(full); err == nil && !info.IsDir() {
				c.File(full)
				return
			}
		}

		// 回退到单页入口
		c.File(filepath.Join(uiDir, "index.html"))
	})

	return r
}

// resolveUIDir 定位前端 ui 目录（依次尝试：环境变量、可执行文件同级、工作目录、项目内）
func resolveUIDir() string {
	if v := os.Getenv("UI_DIR"); v != "" && isDir(v) {
		return v
	}
	candidates := []string{
		filepath.Join("ui"),
		filepath.Join("..", "ui"),
		filepath.Join("..", "tmp", "_fpk_extract", "ui"),
		filepath.Join("tmp", "_fpk_extract", "ui"),
		filepath.Join("..", "..", "tmp", "_fpk_extract", "ui"),
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "ui"),
			filepath.Join(exeDir, "..", "ui"),
		)
	}
	for _, p := range candidates {
		if isDir(p) {
			return p
		}
	}
	return ""
}

// isDir 判断路径是否为存在的目录
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
