package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// 游戏源协议（uogame-source v1）
//
// 本文件实现「源」对外接口，使本服务可作为一个源被任意符合规范的前端接入：
//   - GET /.well-known/uogame-source.json  源描述（公开，供前端自动发现）
//   - GET /api/source/manifest               游戏清单（分页 / 增量）
//   - GET /api/source/search?q=              关键词搜索
//   - GET /api/source/game/:id               单游戏详情
//
// 清单条目采用统一结构（SourceGame），屏蔽内部数据模型，保证协议稳定、可扩展。

// sourceProtocol / sourceVersion 协议标识与版本（对外契约，勿随意变更）
const (
	sourceProtocol = "uogame-source"
	sourceVersion  = 1
)

// wsTopKey 源描述中的端点模板（相对路径，含 {id}/{file} 占位符）
type sourceEndpoints struct {
	Manifest string `json:"manifest"`
	Search   string `json:"search"`
	Detail   string `json:"detail"`
	Cover    string `json:"cover"`
	Images   string `json:"images"`
	Stream   string `json:"stream"`
	Login    string `json:"login"`
}

// sourceCapabilities 能力声明（前端据此开关 UI）
type sourceCapabilities struct {
	Browse  bool `json:"browse"`
	Search  bool `json:"search"`
	Detail  bool `json:"detail"`
	Stream  bool `json:"stream"`
	Offline bool `json:"offline"`
	Local   bool `json:"local"`
}

// sourceAuth 鉴权方式声明
type sourceAuth struct {
	Type string `json:"type"` // bearer / none
}

// sourceDescriptor 源描述（source.json）
type sourceDescriptor struct {
	Protocol     string             `json:"protocol"`
	Version      int                `json:"version"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	BaseURL      string             `json:"baseUrl"`
	Icon         string             `json:"icon,omitempty"`
	Capabilities sourceCapabilities `json:"capabilities"`
	Endpoints    sourceEndpoints    `json:"endpoints"`
	Auth         sourceAuth         `json:"auth"`
}

// SourceGame 对外统一的游戏清单条目（协议契约）
type SourceGame struct {
	ID          uint     `json:"id"`
	Name        string   `json:"name"`
	Cover       string   `json:"cover"`
	GameType    string   `json:"game_type"`
	PlatformTag string   `json:"platform_tag"`
	Tags        []string `json:"tags"`
	Categories  []string `json:"categories"`
	Description string   `json:"description"`
	ReleaseDate string   `json:"release_date"`
	SizeBytes   int64    `json:"size_bytes"`
	AvgRating   float64  `json:"avg_rating"`
	RatingCount int      `json:"rating_count"`
	Aliases     []string `json:"aliases"`
}

// buildSourceDescriptor 构造源描述；baseURL 优先取请求自身来源（便于本机/镜像自动适配）
func buildSourceDescriptor(c *gin.Context) sourceDescriptor {
	base := sourceBaseURL(c)
	return sourceDescriptor{
		Protocol:    sourceProtocol,
		Version:     sourceVersion,
		Name:        getSettingValue("site_name", "拾游集"),
		Description: "拾游集（UOGame）游戏库源",
		BaseURL:     base,
		Capabilities: sourceCapabilities{
			Browse:  true,
			Search:  true,
			Detail:  true,
			Stream:  true,
			Offline: false,
			Local:   false,
		},
		Endpoints: sourceEndpoints{
			Manifest: "/api/source/manifest",
			Search:   "/api/source/search",
			Detail:   "/api/source/game/{id}",
			Cover:    "/api/games/{id}/cover",
			Images:   "/api/games/{id}/images",
			Stream:   "/play/{id}/{file}",
			Login:    "/api/auth/login",
		},
		Auth: sourceAuth{Type: "bearer"},
	}
}

// sourceBaseURL 推导源根地址：优先 X-Forwarded-* / Host，回退为空（前端用相对）
func sourceBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if p := c.GetHeader("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// SourceWellKnown 返回源描述（公开，无需登录）
func SourceWellKnown(c *gin.Context) {
	c.JSON(http.StatusOK, buildSourceDescriptor(c))
}

// SourceManifest 游戏清单：分页（page/page_size）或增量（since 时间戳）
// 返回 { items, total, page, page_size }
func SourceManifest(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := atoiDefault(c.Query("page_size"), 50)
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	base := database.DB.Model(&models.Game{}).Where("is_hidden = 0")

	// 增量：since 为 Unix 秒，仅返回此时间后创建/更新的记录（用 created_at 近似）
	if sinceStr := strings.TrimSpace(c.Query("since")); sinceStr != "" {
		if since, err := strconv.ParseInt(sinceStr, 10, 64); err == nil && since > 0 {
			base = base.Where("created_at > ?", since)
		}
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count games"})
		return
	}

	var games []models.Game
	if err := base.
		Order("sort_order asc, created_at desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&games).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch games"})
		return
	}
	loadGameRelations(games)

	c.JSON(http.StatusOK, gin.H{
		"items":     toSourceGames(games),
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// SourceSearch 关键词搜索：匹配主名或别名
func SourceSearch(c *gin.Context) {
	kw := strings.TrimSpace(c.Query("q"))
	if kw == "" {
		c.JSON(http.StatusOK, gin.H{"items": []SourceGame{}, "total": 0})
		return
	}
	page := atoiDefault(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := atoiDefault(c.Query("page_size"), 50)
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	like := "%" + kw + "%"
	base := database.DB.Model(&models.Game{}).
		Where("is_hidden = 0").
		Where("name LIKE ? OR aliases LIKE ?", like, like)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count games"})
		return
	}

	var games []models.Game
	if err := base.
		Order("sort_order asc, created_at desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&games).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch games"})
		return
	}
	loadGameRelations(games)

	c.JSON(http.StatusOK, gin.H{
		"items":     toSourceGames(games),
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// SourceGameDetail 单游戏详情
func SourceGameDetail(c *gin.Context) {
	id := atoiDefault(c.Param("id"), 0)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid game id"})
		return
	}
	var game models.Game
	if err := database.DB.Where("id = ? AND is_hidden = 0", id).First(&game).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	games := []models.Game{game}
	loadGameRelations(games)
	items := toSourceGames(games)
	c.JSON(http.StatusOK, items[0])
}

// toSourceGames 将内部模型批量映射为协议清单条目
func toSourceGames(games []models.Game) []SourceGame {
	// 分类名映射（分类以 ID 存储，协议对外给名称更通用）
	catNames := categoryNameMap()

	out := make([]SourceGame, 0, len(games))
	for i := range games {
		g := &games[i]
		cats := make([]string, 0, len(g.CategoryIDs))
		for _, cid := range g.CategoryIDs {
			if name, ok := catNames[cid]; ok {
				cats = append(cats, name)
			}
		}
		tags := g.Tags
		if tags == nil {
			tags = []string{}
		}
		aliases := parseAliases(g.Aliases)
		if aliases == nil {
			aliases = []string{}
		}
		out = append(out, SourceGame{
			ID:          g.ID,
			Name:        g.Name,
			Cover:       gameCoverPath(g),
			GameType:    g.GameType,
			PlatformTag: g.PlatformTag,
			Tags:        tags,
			Categories:  cats,
			Description: g.Description,
			ReleaseDate: g.ReleaseDate,
			SizeBytes:   g.SizeBytes,
			AvgRating:   g.AvgRating,
			RatingCount: g.RatingCount,
			Aliases:     aliases,
		})
	}
	return out
}

// gameCoverPath 返回封面接口路径（有封面才给，避免前端拿到空地址）
func gameCoverPath(g *models.Game) string {
	if strings.TrimSpace(g.CoverPath) == "" {
		return ""
	}
	return "/api/games/" + strconv.FormatUint(uint64(g.ID), 10) + "/cover"
}

// categoryNameMap 载入 id → name 映射（供清单条目输出分类名）
func categoryNameMap() map[uint]string {
	var cats []models.Category
	if err := database.DB.Find(&cats).Error; err != nil {
		return map[uint]string{}
	}
	m := make(map[uint]string, len(cats))
	for _, c := range cats {
		m[c.ID] = c.Name
	}
	return m
}

// atoiDefault 解析整数，失败返回默认值
func atoiDefault(s string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return v
	}
	return def
}
