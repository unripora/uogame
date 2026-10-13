package handlers

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// GameConfig 游戏配置文件结构（导入/导出 zip 内的 config.json）
// 兼容：旧字段 category_id/tag 保留；新字段 category_ids/tags 为多值
type GameConfig struct {
	Name          string   `json:"name"`
	CoverPath     string   `json:"cover_path"`
	CategoryID    uint     `json:"category_id"`
	CategoryIDs   []uint   `json:"category_ids,omitempty"`
	CategoryNames []string `json:"category_names,omitempty"` // 可移植键：分类名（跨机导入按名映射）
	SortOrder     int      `json:"sort_order"`
	IsHidden      int      `json:"is_hidden"`
	IsLandscape   int      `json:"is_landscape"`
	Tag           string   `json:"tag"`
	Tags          []string `json:"tags,omitempty"`
	Aliases       []string `json:"aliases,omitempty"` // 别名（曾用名），跨机可移植
}

// GameConfigCategoryIDs 取配置中的分类（优先数组，回退单值）
func (gc *GameConfig) ResolvedCategoryIDs() []uint {
	if len(gc.CategoryIDs) > 0 {
		return normalizeIDs(gc.CategoryIDs)
	}
	if gc.CategoryID > 0 {
		return []uint{gc.CategoryID}
	}
	return nil
}

// ResolvedTags 取配置中的标签（优先数组，回退单值；过滤 default）
func (gc *GameConfig) ResolvedTags() []string {
	src := gc.Tags
	if len(src) == 0 && gc.Tag != "" {
		src = []string{gc.Tag}
	}
	return normalizeTags(src)
}

// normalizeIDs 去重并去掉 0
func normalizeIDs(in []uint) []uint {
	seen := make(map[uint]bool, len(in))
	out := make([]uint, 0, len(in))
	for _, v := range in {
		if v == 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// normalizeTags 去空白、去重、过滤 default
func normalizeTags(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || t == "default" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// categoryNamesByIDs 按 ID 顺序查分类名（用于导出可移植键）
func categoryNamesByIDs(ids []uint) []string {
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	var cats []models.Category
	database.DB.Where("id IN ?", ids).Find(&cats)
	nameMap := make(map[uint]string, len(cats))
	for _, c := range cats {
		nameMap[c.ID] = c.Name
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := strings.TrimSpace(nameMap[id]); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// resolveCategoryIDsByNames 将分类名映射为本机分类 ID（不存在则创建）
func resolveCategoryIDsByNames(names []string) []uint {
	ids := make([]uint, 0, len(names))
	seen := make(map[uint]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		id, err := ensureCategory(name, 0)
		if err != nil || id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// resolveImportCategoryIDs 计算导入时的本机分类 ID 列表
// 优先级：category_names（可移植）→ category_ids → category_id
func resolveImportCategoryIDs(cfg *GameConfig) []uint {
	if len(cfg.CategoryNames) > 0 {
		if ids := resolveCategoryIDsByNames(cfg.CategoryNames); len(ids) > 0 {
			return ids
		}
	}
	return cfg.ResolvedCategoryIDs()
}

// loadGameRelations 批量填充游戏的分类与标签聚合字段
func loadGameRelations(games []models.Game) {
	if len(games) == 0 {
		return
	}
	ids := make([]uint, 0, len(games))
	for _, g := range games {
		ids = append(ids, g.ID)
	}

	// 分类
	var gcs []models.GameCategory
	database.DB.Where("game_id IN ?", ids).Find(&gcs)
	catMap := make(map[uint][]uint)
	for _, gc := range gcs {
		catMap[gc.GameID] = append(catMap[gc.GameID], gc.CategoryID)
	}

	// 标签
	var gts []models.GameTag
	database.DB.Where("game_id IN ?", ids).Find(&gts)
	tagMap := make(map[uint][]string)
	for _, gt := range gts {
		tagMap[gt.GameID] = append(tagMap[gt.GameID], gt.Tag)
	}

	for i := range games {
		games[i].CategoryIDs = catMap[games[i].ID]
		games[i].Tags = tagMap[games[i].ID]
		if games[i].CategoryIDs == nil {
			games[i].CategoryIDs = []uint{}
		}
		if games[i].Tags == nil {
			games[i].Tags = []string{}
		}
	}
}

// saveGameRelations 重写游戏的多值关联（分类 + 标签），并同步旧字段首项
func saveGameRelations(gameID uint, categoryIDs []uint, tags []string) {
	categoryIDs = normalizeIDs(categoryIDs)
	tags = normalizeTags(tags)

	// 分类：重建
	_ = database.DB.Where("game_id = ?", gameID).Delete(&models.GameCategory{}).Error
	for _, cid := range categoryIDs {
		_ = database.DB.Create(&models.GameCategory{GameID: gameID, CategoryID: cid}).Error
	}
	// 标签：重建
	_ = database.DB.Where("game_id = ?", gameID).Delete(&models.GameTag{}).Error
	for _, t := range tags {
		_ = database.DB.Create(&models.GameTag{GameID: gameID, Tag: t}).Error
	}

	// 同步旧字段 = 首项（兼容）
	var legacyCat uint
	if len(categoryIDs) > 0 {
		legacyCat = categoryIDs[0]
	}
	legacyTag := "default"
	if len(tags) > 0 {
		legacyTag = tags[0]
	}
	_ = database.DB.Model(&models.Game{}).Where("id = ?", gameID).
		Updates(map[string]any{"category_id": legacyCat, "tag": legacyTag}).Error
}

// addGameCategory 追加一个分类关联（去重）
func addGameCategory(gameID, categoryID uint) {
	if categoryID == 0 {
		return
	}
	var cnt int64
	database.DB.Model(&models.GameCategory{}).
		Where("game_id = ? AND category_id = ?", gameID, categoryID).Count(&cnt)
	if cnt == 0 {
		_ = database.DB.Create(&models.GameCategory{GameID: gameID, CategoryID: categoryID}).Error
	}
	// 同步旧字段：旧值为空时写首项
	var g models.Game
	if err := database.DB.First(&g, gameID).Error; err == nil && g.CategoryID == 0 {
		_ = database.DB.Model(&models.Game{}).Where("id = ?", gameID).
			Update("category_id", categoryID).Error
	}
}

// removeGameCategories 批量移除游戏的指定分类关联
func removeGameCategories(gameID uint, categoryIDs []uint) {
	if len(categoryIDs) == 0 {
		return
	}
	_ = database.DB.Where("game_id = ? AND category_id IN ?", gameID, categoryIDs).
		Delete(&models.GameCategory{}).Error
	// 同步旧字段 = 剩余首项
	var rows []models.GameCategory
	database.DB.Where("game_id = ?", gameID).Find(&rows)
	var first uint
	if len(rows) > 0 {
		first = rows[0].CategoryID
	}
	_ = database.DB.Model(&models.Game{}).Where("id = ?", gameID).
		Update("category_id", first).Error
}

// ListTags 返回全部标签（去重，按出现频次降序）
// GET /api/tags
func ListTags(c *gin.Context) {
	var tags []string
	database.DB.Model(&models.GameTag{}).
		Select("tag").
		Group("tag").
		Order("COUNT(*) desc, tag asc").
		Pluck("tag", &tags)
	if tags == nil {
		tags = []string{}
	}
	c.JSON(http.StatusOK, tags)
}

// ListGames 列出游戏
// 兼容两种调用：
//   - 不带 page 参数：返回**裸数组**（全量，保持旧契约，供第三方/旧的增量同步使用）
//   - 带 page 参数：返回分页对象 {items,total,page,page_size}（供管理面板等大数据量场景）
//
// 预排序：sort_order 升序、created_at 降序
func ListGames(c *gin.Context) {
	pageStr := c.Query("page")
	pageSizeStr := c.Query("page_size")

	// 分页模式
	if pageStr != "" || pageSizeStr != "" {
		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		pageSize, _ := strconv.Atoi(pageSizeStr)
		if pageSize < 1 {
			pageSize = 20
		}
		if pageSize > 200 {
			pageSize = 200
		}

		// 关键词过滤（匹配主名或别名；别名以 JSON 数组字符串存储）
		kw := strings.TrimSpace(c.Query("q"))
		base := database.DB.Model(&models.Game{})
		if kw != "" {
			like := "%" + kw + "%"
			base = base.Where("name LIKE ? OR aliases LIKE ?", like, like)
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
		backfillGameSizes(games)
		if games == nil {
			games = []models.Game{}
		}
		c.JSON(http.StatusOK, gin.H{
			"items":     games,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		})
		return
	}

	// 全量模式（兼容旧契约）
	var games []models.Game
	if err := database.DB.Order("sort_order asc, created_at desc").Find(&games).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch games"})
		return
	}
	loadGameRelations(games)
	backfillGameSizes(games)
	c.JSON(http.StatusOK, games)
}

// CreateGame 新建游戏
func CreateGame(c *gin.Context) {
	var game models.Game
	if err := c.ShouldBindJSON(&game); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if game.CreatedAt == 0 {
		game.CreatedAt = time.Now().Unix()
	}
	// 多值兼容：若未显式给 category_ids/tags，则用旧单值兜底
	categoryIDs := game.CategoryIDs
	if len(categoryIDs) == 0 && game.CategoryID > 0 {
		categoryIDs = []uint{game.CategoryID}
	}
	tags := game.Tags
	if len(tags) == 0 && game.Tag != "" && game.Tag != "default" {
		tags = []string{game.Tag}
	}
	// 旧字段同步首项
	categoryIDs = normalizeIDs(categoryIDs)
	tags = normalizeTags(tags)
	game.CategoryID = 0
	if len(categoryIDs) > 0 {
		game.CategoryID = categoryIDs[0]
	}
	game.Tag = "default"
	if len(tags) > 0 {
		game.Tag = tags[0]
	}
	game.CategoryIDs = nil
	game.Tags = nil

	// mount_path 兜底：为空或相对路径时，落到游戏存储目录下
	game.MountPath = resolveOrCreateMount(game.Name, game.MountPath)
	if err := database.DB.Create(&game).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create game"})
		return
	}
	saveGameRelations(game.ID, categoryIDs, tags)
	game.CategoryIDs = categoryIDs
	game.Tags = tags
	c.JSON(http.StatusOK, gin.H{"message": "创建成功", "game": game})
}

// resolveOrCreateMount 计算游戏的挂载绝对路径
//   - 已是绝对路径：原样返回
//   - 相对/空：以 games/{名称} 为目录，相对值作为其下文件名
func resolveOrCreateMount(name, mountPath string) string {
	if mountPath != "" && filepath.IsAbs(mountPath) {
		return filepath.Clean(mountPath)
	}
	dir := filepath.Join(gamesDir(), sanitizeDirName(name))
	if mountPath == "" {
		return dir
	}
	return filepath.Join(dir, filepath.Base(mountPath))
}

// UpdateGame 更新游戏
func UpdateGame(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	var payload models.Game
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	payload.ID = game.ID
	payload.CreatedAt = game.CreatedAt

	// 未在 payload 中提供的字段，用原记录兜底（避免 Save 全字段覆盖清空）
	if payload.MountPath == "" {
		payload.MountPath = game.MountPath
	}
	if payload.CoverPath == "" {
		payload.CoverPath = game.CoverPath
	}
	if payload.PlatformTag == "" {
		payload.PlatformTag = game.PlatformTag
	}
	// ContentHash/HotTime 为系统维护字段，始终保留原值（除非显式传入）
	if payload.ContentHash == "" {
		payload.ContentHash = game.ContentHash
	}
	if payload.HotTime == 0 {
		payload.HotTime = game.HotTime
	}
	// 刮削元数据字段：编辑时若未传入则保留原值，避免清空
	if payload.Description == "" {
		payload.Description = game.Description
	}
	if payload.ReleaseDate == "" {
		payload.ReleaseDate = game.ReleaseDate
	}
	if payload.ExternalSource == "" {
		payload.ExternalSource = game.ExternalSource
	}
	// 别名：编辑时若未传入则保留原值，避免 Save 全字段覆盖清空
	if payload.Aliases == "" {
		payload.Aliases = game.Aliases
	}

	// 多值兼容：若未显式给数组，则用旧单值兜底
	categoryIDs := payload.CategoryIDs
	if len(categoryIDs) == 0 && payload.CategoryID > 0 {
		categoryIDs = []uint{payload.CategoryID}
	}
	tags := payload.Tags
	if len(tags) == 0 && payload.Tag != "" && payload.Tag != "default" {
		tags = []string{payload.Tag}
	}
	categoryIDs = normalizeIDs(categoryIDs)
	tags = normalizeTags(tags)
	payload.CategoryID = 0
	if len(categoryIDs) > 0 {
		payload.CategoryID = categoryIDs[0]
	}
	payload.Tag = "default"
	if len(tags) > 0 {
		payload.Tag = tags[0]
	}
	payload.CategoryIDs = nil
	payload.Tags = nil

	if err := database.DB.Save(&payload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update game"})
		return
	}
	saveGameRelations(payload.ID, categoryIDs, tags)
	payload.CategoryIDs = categoryIDs
	payload.Tags = tags
	c.JSON(http.StatusOK, gin.H{"message": "更新成功", "game": payload})
}

// DeleteGame 删除游戏
func DeleteGame(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := database.DB.Delete(&models.Game{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete game"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// GetGameCover 返回游戏封面图片
func GetGameCover(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	if game.CoverPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cover not found"})
		return
	}
	// 兼容历史数据：cover_path 仍为 data URI 时直接解码返回
	if isImageDataURI(game.CoverPath) {
		mime := "image/png"
		if idx := strings.Index(game.CoverPath, ";"); idx > len("data:") {
			mime = game.CoverPath[len("data:"):idx]
		}
		_, data, ok := decodeImageDataURI(game.CoverPath)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Cover not found"})
			return
		}
		c.Data(http.StatusOK, mime, data)
		return
	}
	path := resolveGamePath(game.CoverPath)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cover not found"})
		return
	}
	c.File(path)
}

// GameConfigHandler 返回单个游戏的配置（GET /games/config.json?id=）
func GameConfigHandler(c *gin.Context) {
	id, _ := strconv.Atoi(c.Query("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	c.JSON(http.StatusOK, GameConfig{
		Name:        game.Name,
		CoverPath:   game.CoverPath,
		CategoryID:  game.CategoryID,
		SortOrder:   game.SortOrder,
		IsHidden:    game.IsHidden,
		IsLandscape: game.IsLandscape,
		Tag:         game.Tag,
	})
}

// GetGamesStats 返回游戏统计信息
// 前端约定：{count:number, last_updated:number}
func GetGamesStats(c *gin.Context) {
	var count int64
	database.DB.Model(&models.Game{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{
		"count":        count,
		"last_updated": lastUpdatedAt("games"),
	})
}

// GetGamesChanges 增量变更接口：返回自 since 之后新增/更新的游戏及被删除的 ID
func GetGamesChanges(c *gin.Context) {
	since, _ := strconv.ParseInt(c.Query("since"), 10, 64)
	var games []models.Game
	if since > 0 {
		database.DB.Where("created_at > ?", since).Find(&games)
	} else {
		database.DB.Find(&games)
	}
	c.JSON(http.StatusOK, gin.H{
		"upserts":      games,
		"removed_ids":  []uint{},
		"last_updated": lastUpdatedAt("games"),
	})
}

// BatchExportGames 批量导出多个游戏为单个 zip
func BatchExportGames(c *gin.Context) {
	var payload struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || len(payload.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename=\"games-batch.zip\"")
	zw := zip.NewWriter(c.Writer)
	defer zw.Close()

	for _, id := range payload.IDs {
		var game models.Game
		if err := database.DB.First(&game, id).Error; err != nil {
			continue
		}
		rels := []models.Game{game}
		loadGameRelations(rels)
		g := rels[0]
		coverRel := embedCoverForExport(zw, g.CoverPath, fmt.Sprintf("%d/", id))
		_ = writeZipJSON(zw, fmt.Sprintf("%d/config.json", id), GameConfig{
			Name:          g.Name,
			CoverPath:     coverRel,
			CategoryID:    g.CategoryID,
			CategoryIDs:   g.CategoryIDs,
			CategoryNames: categoryNamesByIDs(g.CategoryIDs),
			SortOrder:     g.SortOrder,
			IsHidden:      g.IsHidden,
			IsLandscape:   g.IsLandscape,
			Tag:           g.Tag,
			Tags:          g.Tags,
			Aliases:       parseAliases(g.Aliases),
		})
		base := resolveGamePath(game.MountPath)
		if info, err := os.Stat(base); err == nil {
			if info.IsDir() {
				_ = writeZipDir(zw, base, fmt.Sprintf("%d/files", id))
			} else {
				_ = writeZipFile(zw, base, fmt.Sprintf("%d/files/%s", id, filepath.Base(base)))
			}
		}
	}
}

// UploadGameRoms 上传 ROM 文件到游戏挂载目录
func UploadGameRoms(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	base := resolveGamePath(game.MountPath)
	if base == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Game mount path not configured"})
		return
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}
	dst := filepath.Join(base, filepath.Base(fileHeader.Filename))
	src, oerr := fileHeader.Open()
	if oerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()
	out, cerr := os.Create(dst)
	if cerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	// 按 ROM 文件类型补充平台标签（去重追加）
	if pt := platformTagForFile(fileHeader.Filename); pt != "" {
		if !strings.Contains(game.PlatformTag, pt) {
			merged := strings.TrimSpace(game.PlatformTag + " " + pt)
			_ = database.DB.Model(&models.Game{}).Where("id = ?", game.ID).
				Update("platform_tag", merged).Error
		}
	}
	// 重新计算体积（按大小排序用）
	if size := computeGameSize(base); size > 0 {
		_ = database.DB.Model(&models.Game{}).Where("id = ?", game.ID).
			Update("size_bytes", size).Error
	}
	c.JSON(http.StatusOK, gin.H{"message": "上传成功", "file": filepath.Base(dst)})
}

// ListGameRoms 列出游戏挂载目录下的 ROM 文件（供 emulator 选择）
// 前端约定：返回**裸数组**，元素为相对挂载目录的文件路径（以 / 分隔），
// 该相对路径可直接拼接为 /play/{id}/{path} 供 iframe 加载
// 扫描深度：4 层（用户决策 Q1）
func ListGameRoms(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	base := resolveGamePath(game.MountPath)
	if base == "" {
		c.JSON(http.StatusOK, []string{})
		return
	}
	info, err := os.Stat(base)
	if err != nil {
		// 挂载目录不可访问时返回空数组，前端会提示"未检测到 ROM"
		c.JSON(http.StatusOK, []string{})
		return
	}
	// 单文件挂载：直接返回文件名
	if !info.IsDir() {
		c.JSON(http.StatusOK, []string{filepath.Base(base)})
		return
	}

	found := scanRoms(base, "", maxScanDepth)
	if found == nil {
		found = []string{}
	}
	c.JSON(http.StatusOK, found)
}

// scanRoms 递归扫描目录查找 ROM 文件，返回相对 base 的路径（以 / 分隔），最大深度 maxDepth
func scanRoms(dir, prefix string, maxDepth int) []string {
	if maxDepth < 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		rel := name
		if prefix != "" {
			rel = prefix + "/" + name
		}
		if e.IsDir() {
			result = append(result, scanRoms(filepath.Join(dir, name), rel, maxDepth-1)...)
			continue
		}
		if detectTypeByExt(name) != "" {
			result = append(result, rel)
		}
	}
	return result
}

// gameImageExts 详情页轮播可用的图片扩展名（白名单）
var gameImageExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
	".gif":  true,
	".bmp":  true,
}

// coverKeywords 文件名中命中这些关键词的图片优先作为轮播首图
var coverKeywords = []string{"cover", "logo", "icon", "title", "banner", "startup", "splash"}

// GetGameImages 返回游戏详情页可用的图片列表（相对挂载目录的路径）
// 策略：仅扫描挂载目录内的图片文件（白名单扩展名），按「封面优先 + 文件体积降序」排序返回。
// 返回项为相对路径，前端用 `/play/{id}/{path}` 加载。
func GetGameImages(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}

	base := resolveGamePath(game.MountPath)
	if base == "" {
		base = filepath.Join(gamesDir(), sanitizeDirName(game.Name))
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		c.JSON(http.StatusOK, gin.H{"images": []string{}})
		return
	}

	type imgEntry struct {
		rel     string
		size    int64
		isCover bool
	}
	found := make([]imgEntry, 0, 32)
	_ = filepath.Walk(base, func(p string, fi os.FileInfo, werr error) error {
		if werr != nil || fi == nil {
			return nil
		}
		if fi.IsDir() {
			// 跳过隐藏目录
			if strings.HasPrefix(fi.Name(), ".") && p != base {
				return filepath.SkipDir
			}
			return nil
		}
		ext := toLower(filepath.Ext(fi.Name()))
		if !gameImageExts[ext] {
			return nil
		}
		rel, rerr := filepath.Rel(base, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		lower := toLower(fi.Name())
		isCover := false
		for _, kw := range coverKeywords {
			if strings.Contains(lower, kw) {
				isCover = true
				break
			}
		}
		found = append(found, imgEntry{rel: rel, size: fi.Size(), isCover: isCover})
		return nil
	})

	// 排序：封面类优先 → 体积从大到小（大图更适合轮播）
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].isCover != found[j].isCover {
			return found[i].isCover
		}
		return found[i].size > found[j].size
	})

	const maxImages = 12
	images := make([]string, 0, maxImages)
	seen := make(map[string]bool, maxImages)
	for _, e := range found {
		if seen[e.rel] {
			continue
		}
		seen[e.rel] = true
		images = append(images, e.rel)
		if len(images) >= maxImages {
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"images": images})
}

// lastUpdatedAt 返回指定资源的最新更新时间（Unix 秒）；无数据时返回 0
func lastUpdatedAt(resource string) int64 {
	var ts int64
	switch resource {
	case "games":
		database.DB.Model(&models.Game{}).Select("COALESCE(MAX(created_at), 0)").Scan(&ts)
	}
	return ts
}

// ServeGameFile 提供游戏 ROM / H5 文件访问
func ServeGameFile(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	rel := c.Param("file")
	base := resolveGamePath(game.MountPath)
	if base == "" {
		// 回退：按约定目录 games/{名称} 探测（兼容早期未写 mount_path 的存量数据）
		base = filepath.Join(gamesDir(), sanitizeDirName(game.Name))
	}
	if _, err := os.Stat(base); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gameFileErrorMsg})
		return
	}
	full := filepath.Join(base, filepath.Clean("/"+rel))
	if !strings.HasPrefix(full, base) {
		// 越权路径统一返回 404（掩码），不暴露授权边界
		c.JSON(http.StatusNotFound, gin.H{"error": gameFileErrorMsg})
		return
	}
	// 目录型 H5：自动补 index.html
	if info, err := os.Stat(full); err == nil && info.IsDir() {
		full = filepath.Join(full, "index.html")
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"error": gameFileErrorMsg})
		return
	}

	// Content-Type 从扩展名白名单推导（不信任任意扩展名，防 content-sniffing）
	c.Header("Content-Type", contentTypeForGameFile(full))
	c.Header("X-Content-Type-Options", "nosniff")

	// H5 页面：注入存档命名空间代理脚本，使该游戏的 localStorage/IndexedDB 可归属。
	// 注入需读入改写，故放弃该文件的 Range（HTML 很小，可接受）。
	if ext := toLower(filepath.Ext(full)); ext == ".html" || ext == ".htm" {
		data, rerr := os.ReadFile(full)
		if rerr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": gameFileErrorMsg})
			return
		}
		c.String(http.StatusOK, injectSaveNamespace(string(data), fmt.Sprintf("h5g%d", game.ID)))
		return
	}

	f, err := os.Open(full)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gameFileErrorMsg})
		return
	}
	defer f.Close()
	// http.ServeContent 原生支持 Range / If-Modified-Since，利于大 ROM 分段加载
	http.ServeContent(c.Writer, c.Request, filepath.Base(full), info.ModTime(), f)
}

// gameFileMimeTypes 游戏文件扩展名 → MIME 类型（可信来源，仅覆盖必要类型）
var gameFileMimeTypes = map[string]string{
	".swf":   "application/x-shockwave-flash",
	".wasm":  "application/wasm",
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".svg":   "image/svg+xml",
	".mp3":   "audio/mpeg",
	".ogg":   "audio/ogg",
	".wav":   "audio/wav",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".txt":   "text/plain; charset=utf-8",
	".xml":   "application/xml; charset=utf-8",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
}

// contentTypeForGameFile 依据扩展名返回 MIME；未知类型回退 application/octet-stream
func contentTypeForGameFile(name string) string {
	ext := toLower(filepath.Ext(name))
	if mime, ok := gameFileMimeTypes[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

// gameFileErrorMsg 复刻原版错误文案（存储权限提示）
const gameFileErrorMsg = "读取游戏文件失败，权限不足！请前往 fnOS 应用中心 -> 已安装应用 -> 拾游集设置，为该应用的游戏存储文件夹授予读取与写入权限。"

// ExportGame 导出游戏为 zip（包含 config.json 与游戏文件本体）
func ExportGame(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", game.Name+".zip"))

	zw := zip.NewWriter(c.Writer)
	defer zw.Close()

	// 写入 config.json（双写旧字段 + 新数组，保证新旧导入器都能识别）
	rels := []models.Game{game}
	loadGameRelations(rels)
	g := rels[0]
	coverRel := embedCoverForExport(zw, g.CoverPath, "")
	if err := writeZipJSON(zw, "config.json", GameConfig{
		Name:          g.Name,
		CoverPath:     coverRel,
		CategoryID:    g.CategoryID,
		CategoryIDs:   g.CategoryIDs,
		CategoryNames: categoryNamesByIDs(g.CategoryIDs),
		SortOrder:     g.SortOrder,
		IsHidden:      g.IsHidden,
		IsLandscape:   g.IsLandscape,
		Tag:           g.Tag,
		Tags:          g.Tags,
		Aliases:       parseAliases(g.Aliases),
	}); err != nil {
		return
	}

	// 写入游戏文件本体（若存在）
	base := resolveGamePath(game.MountPath)
	if info, err := os.Stat(base); err == nil {
		if info.IsDir() {
			_ = writeZipDir(zw, base, "files")
		} else {
			_ = writeZipFile(zw, base, "files/"+filepath.Base(base))
		}
	}
}

// ImportGame 从上传的 zip 导入游戏
// 流程：保存临时文件 → 解析 config.json → 去重判定 → 解包游戏文件到存储目录 → 写 mount_path → 建库记录
func ImportGame(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	onDuplicate := c.PostForm("on_duplicate") // skip(默认) | overwrite
	if onDuplicate != "overwrite" {
		onDuplicate = "skip"
	}

	tmp, terr := saveUploadTemp(fileHeader)
	if terr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	defer removeTemp(tmp)

	zr, zerr := zip.OpenReader(tmp)
	if zerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid zip file"})
		return
	}
	defer zr.Close()

	var cfg *GameConfig
	found := false
	for _, f := range zr.File {
		if f.Name == "config.json" {
			found = true
			if gc, ok := decodeGameConfig(f); ok {
				cfg = gc
			}
			break
		}
	}
	// 多游戏包：根无 config.json，但含 <数字>/config.json（来自批量导出）
	if !found && isBatchZip(zr.File) {
		importBatchZip(c, zr.File, onDuplicate)
		return
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config.json not found in zip"})
		return
	}
	if cfg == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config.json 解析失败：文件不是合法 UTF-8 无 BOM 的 JSON"})
		return
	}

	hash := zipContentHash(zr.File)
	platformTag := platformTagFromZip(zr.File)

	// === 去重判定（仅按内容指纹）===
	if dup := findDuplicate(hash, cfg.Name); dup != nil {
		existing := dup.Existing
		// 指纹相同但名称不同 → 不视为重复，把新名称追加为「别名」
		if normalizeName(existing.Name) != normalizeName(cfg.Name) {
			if list, added := appendAlias(existing, cfg.Name); added {
				_ = database.DB.Model(&models.Game{}).Where("id = ?", existing.ID).
					Update("aliases", encodeAliases(list)).Error
				existing.Aliases = encodeAliases(list)
				c.JSON(http.StatusOK, gin.H{
					"message":   fmt.Sprintf("已追加别名（属于「%s」）", existing.Name),
					"duplicate": true,
					"aliased":   true,
					"reason":    dup.Reason,
					"existing":  existing,
				})
				return
			}
			// 别名已存在 → 按 skip/overwrite 处理
		}
		if onDuplicate == "skip" {
			c.JSON(http.StatusOK, gin.H{
				"message":   fmt.Sprintf("已跳过重复游戏（与「%s」重复）", existing.Name),
				"duplicate": true,
				"skipped":   true,
				"reason":    dup.Reason,
				"existing":  existing,
			})
			return
		}
		// overwrite：解包覆盖到既有记录的 mount_path（避免生成新目录）
		dir, derr := unpackGame(zr.File, existing.Name, existing.ID, existing.MountPath)
		if derr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to extract game files"})
			return
		}
		// 覆盖既有记录的可变字段
		updates := map[string]interface{}{
			"name":         cfg.Name,
			"cover_path":   cfg.CoverPath,
			"mount_path":   dir,
			"tag":          cfg.Tag,
			"content_hash": hash,
			"size_bytes":   computeGameSize(dir),
		}
		// 封面处理：data URI 解码落盘 / 包内候选图提取（覆盖原 base64/空值）
		if cover := resolveImportedCover(cfg, zr.File, dir, ""); cover != "" {
			updates["cover_path"] = cover
		}
		if platformTag != "" {
			updates["platform_tag"] = platformTag
		}
		if cfg.SortOrder != 0 {
			updates["sort_order"] = cfg.SortOrder
		}
		if cfg.IsLandscape != 0 {
			updates["is_landscape"] = cfg.IsLandscape
		}
		if err := database.DB.Model(&models.Game{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update database"})
			return
		}
		// 同步多值关联（分类 + 标签）
		resolvedCats := resolveImportCategoryIDs(cfg)
		saveGameRelations(existing.ID, resolvedCats, cfg.ResolvedTags())
		var out models.Game
		_ = database.DB.First(&out, existing.ID).Error
		loadGameRelations([]models.Game{out})
		out.CategoryIDs = resolvedCats
		out.Tags = cfg.ResolvedTags()
		c.JSON(http.StatusOK, gin.H{
			"message":     fmt.Sprintf("已覆盖重复游戏（原「%s」）", existing.Name),
			"duplicate":   true,
			"overwritten": true,
			"reason":      dup.Reason,
			"game":        out,
		})
		return
	}

	// === 非重复：解包 + 建库 ===
	game, gerr := buildGameFromConfig(cfg, hash)
	if gerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare game files"})
		return
	}
	dir, derr := unpackGame(zr.File, cfg.Name, 0, "")
	if derr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to extract game files"})
		return
	}
	game.MountPath = dir
	game.PlatformTag = platformTag
	game.SizeBytes = computeGameSize(dir)
	// 封面处理：data URI 解码落盘 / 包内候选图提取
	if cover := resolveImportedCover(cfg, zr.File, dir, ""); cover != "" {
		game.CoverPath = cover
	}

	// 白名单自动分类：仅在未指定分类时生效，命中后回传 auto_detected 供前端提示
	autoDetected := false
	catIDs := resolveImportCategoryIDs(cfg)
	tags := cfg.ResolvedTags()
	if len(catIDs) == 0 {
		if catID := autoClassifyGame(game.Name, strings.Join(tags, " ")); catID != 0 {
			game.CategoryID = catID
			catIDs = []uint{catID}
			autoDetected = true
		}
	}
	if err := database.DB.Create(&game).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	saveGameRelations(game.ID, catIDs, tags)
	game.CategoryIDs = catIDs
	if game.CategoryIDs == nil {
		game.CategoryIDs = []uint{}
	}
	game.Tags = tags
	if game.Tags == nil {
		game.Tags = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"message": "导入成功", "game": game, "auto_detected": autoDetected})
}

// sourceGameTypeByCode 平台码 → game_type 值（与 emulator 页 coreMapping / 前端 GAME_TYPES 对齐）
var sourceGameTypeByCode = map[string]string{
	"nes": "FC", "gb": "GB", "gba": "GBA", "snes": "SNES",
	"segaMD": "MD", "sega32x": "SEGA32X", "segaMS": "SMS", "segaGG": "GG",
	"pce": "PCE", "ngp": "NGP", "ws": "WS", "vb": "VB",
	"n64": "N64", "nds": "NDS", "psx": "PSX",
	"atari2600": "A2600", "atari5200": "A5200", "atari7800": "A7800",
	"coleco": "COLECO", "flash": "FLASH", "h5": "H5",
}

// gameTypeFromFileName 依据文件名扩展名推断 game_type（未识别返回空串）
func gameTypeFromFileName(name string) string {
	code := detectTypeByExt(name)
	if code == "" {
		return ""
	}
	return sourceGameTypeByCode[code]
}

// existingBaseDir 求既有游戏的可写目录（挂载为文件时取其所在目录）
func existingBaseDir(g *models.Game) string {
	base := resolveGamePath(g.MountPath)
	if base == "" {
		base = filepath.Join(gamesDir(), sanitizeDirName(g.Name))
	}
	if info, err := os.Stat(base); err == nil && !info.IsDir() {
		base = filepath.Dir(base)
	}
	return base
}

// placeSourceFile 将临时文件写入目标目录下的指定文件名（同名覆盖）
func placeSourceFile(tmpPath, dir, filename string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(dir, filepath.Base(filename))
	in, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ImportSourceFile 单源文件导入（ROM / Flash(SWF) / HTML 单文件）
// POST /api/games/source（multipart）
// 表单：file（必填）、name、game_type、category_id、tags（逗号分隔）、is_landscape、on_duplicate
// 落盘：目录挂载 games/{名称}/；.html/.htm 统一存为 index.html（保证 H5 路由 /play/{id}/index.html 命中）
func ImportSourceFile(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	origName := filepath.Base(fileHeader.Filename)
	code := detectTypeByExt(origName)
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "不支持的文件类型，请上传 ROM / SWF / HTML 单文件，或改用游戏包(.zip)导入",
		})
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = strings.TrimSuffix(origName, filepath.Ext(origName))
	}
	gameType := strings.ToUpper(strings.TrimSpace(c.PostForm("game_type")))
	if gameType == "" {
		gameType = gameTypeFromFileName(origName)
	}
	onDuplicate := c.PostForm("on_duplicate")
	if onDuplicate != "overwrite" {
		onDuplicate = "skip"
	}
	isLandscape := 0
	if c.PostForm("is_landscape") == "1" {
		isLandscape = 1
	}

	// 暂存上传内容并流式计算指纹（避免大 ROM 全量入内存）
	tmp, terr := os.CreateTemp("", "srcimport-*")
	if terr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	src, oerr := fileHeader.Open()
	if oerr != nil {
		tmp.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	hasher := sha256.New()
	if _, cerr := io.Copy(io.MultiWriter(tmp, hasher), src); cerr != nil {
		src.Close()
		tmp.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	src.Close()
	tmp.Close()
	hash := hex.EncodeToString(hasher.Sum(nil))[:16]

	platformTag := platformTagForFile(origName)
	// HTML/HTM 单文件统一存为 index.html，保证 H5 播放路由命中
	destName := origName
	if code == "h5" {
		destName = "index.html"
	}

	// 分类 / 标签
	var categoryIDs []uint
	if cid, _ := strconv.Atoi(c.PostForm("category_id")); cid > 0 {
		categoryIDs = []uint{uint(cid)}
	}
	tags := normalizeTags(strings.Split(c.PostForm("tags"), ","))

	// === 去重判定（仅按内容指纹）===
	if dup := findDuplicate(hash, name); dup != nil {
		existing := dup.Existing
		// 指纹相同但名称不同 → 把新名称追加为「别名」
		if normalizeName(existing.Name) != normalizeName(name) {
			if list, added := appendAlias(existing, name); added {
				_ = database.DB.Model(&models.Game{}).Where("id = ?", existing.ID).
					Update("aliases", encodeAliases(list)).Error
				existing.Aliases = encodeAliases(list)
				c.JSON(http.StatusOK, gin.H{
					"message":   fmt.Sprintf("已追加别名（属于「%s」）", existing.Name),
					"duplicate": true,
					"aliased":   true,
					"reason":    dup.Reason,
					"existing":  existing,
				})
				return
			}
		}
		if onDuplicate == "skip" {
			c.JSON(http.StatusOK, gin.H{
				"message":   fmt.Sprintf("已跳过重复游戏（与「%s」重复）", existing.Name),
				"duplicate": true,
				"skipped":   true,
				"reason":    dup.Reason,
				"existing":  existing,
			})
			return
		}
		// overwrite：写入既有记录的挂载目录并覆盖可变字段
		dir := existingBaseDir(existing)
		if perr := placeSourceFile(tmpPath, dir, destName); perr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
			return
		}
		updates := map[string]interface{}{
			"name":         name,
			"mount_path":   dir,
			"game_type":    gameType,
			"content_hash": hash,
			"size_bytes":   computeGameSize(dir),
			"is_landscape": isLandscape,
		}
		if platformTag != "" {
			updates["platform_tag"] = platformTag
		}
		if err := database.DB.Model(&models.Game{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update database"})
			return
		}
		saveGameRelations(existing.ID, categoryIDs, tags)
		var out models.Game
		_ = database.DB.First(&out, existing.ID).Error
		loadGameRelations([]models.Game{out})
		out.CategoryIDs = categoryIDs
		out.Tags = tags
		c.JSON(http.StatusOK, gin.H{
			"message":     fmt.Sprintf("已覆盖重复游戏（原「%s」）", existing.Name),
			"duplicate":   true,
			"overwritten": true,
			"reason":      dup.Reason,
			"game":        out,
		})
		return
	}

	// === 非重复：落盘 + 建库 ===
	dir := resolveOrCreateMount(name, "")
	if perr := placeSourceFile(tmpPath, dir, destName); perr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	var legacyCat uint
	if len(categoryIDs) > 0 {
		legacyCat = categoryIDs[0]
	}
	legacyTag := "default"
	if len(tags) > 0 {
		legacyTag = tags[0]
	}
	game := models.Game{
		Name:        name,
		MountPath:   dir,
		GameType:    gameType,
		PlatformTag: platformTag,
		ContentHash: hash,
		CategoryID:  legacyCat,
		Tag:         legacyTag,
		IsLandscape: isLandscape,
		SizeBytes:   computeGameSize(dir),
		CreatedAt:   time.Now().Unix(),
	}
	if err := database.DB.Create(&game).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	saveGameRelations(game.ID, categoryIDs, tags)
	game.CategoryIDs = categoryIDs
	if game.CategoryIDs == nil {
		game.CategoryIDs = []uint{}
	}
	game.Tags = tags
	if game.Tags == nil {
		game.Tags = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"message": "导入成功", "game": game})
}

// buildGameFromConfig 依据 config.json 构造 Game（未含 mount_path）
// 兼容旧包：category_ids 缺失时回退 category_id；tags 缺失时回退 tag
func buildGameFromConfig(cfg *GameConfig, hash string) (models.Game, error) {
	catIDs := resolveImportCategoryIDs(cfg)
	tags := cfg.ResolvedTags()
	var legacyCat uint
	if len(catIDs) > 0 {
		legacyCat = catIDs[0]
	}
	legacyTag := "default"
	if len(tags) > 0 {
		legacyTag = tags[0]
	}
	return models.Game{
		Name:        cfg.Name,
		CoverPath:   cfg.CoverPath,
		CategoryID:  legacyCat,
		SortOrder:   cfg.SortOrder,
		IsHidden:    cfg.IsHidden,
		IsLandscape: cfg.IsLandscape,
		Tag:         legacyTag,
		ContentHash: hash,
		Aliases:     encodeAliases(cfg.Aliases),
		CreatedAt:   time.Now().Unix(),
	}, nil
}

// platformTagFromZip 依据 zip 内条目名推断平台标签（供导入时自动打标签）
func platformTagFromZip(files []*zip.File) string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		name := strings.TrimPrefix(f.Name, "./")
		if strings.HasSuffix(name, "/") { // 目录条目跳过
			continue
		}
		names = append(names, name)
	}
	return platformTagFromFiles(names)
}

// isBatchZip 判定是否为多游戏包：根无 config.json，且存在 <数字>/config.json
func isBatchZip(files []*zip.File) bool {
	for _, f := range files {
		name := strings.TrimPrefix(f.Name, "./")
		if strings.HasSuffix(name, "/config.json") {
			seg := strings.TrimSuffix(name, "/config.json")
			if seg != "" && isDigits(seg) { // 顶层为纯数字目录（导出格式）
				return true
			}
		}
	}
	return false
}

// extractChildEntries 取出属于指定子目录前缀（如 "13/"）的全部条目
func extractChildEntries(files []*zip.File, prefix string) []*zip.File {
	out := make([]*zip.File, 0)
	for _, f := range files {
		name := strings.TrimPrefix(f.Name, "./")
		if strings.HasPrefix(name, prefix) {
			out = append(out, f)
		}
	}
	return out
}

// importBatchZip 多游戏包导入：逐个子目录（<id>/config.json + <id>/files/...）导入
// 每个子游戏重新分配 ID；逐游戏去重；支持 skip/overwrite
func importBatchZip(c *gin.Context, files []*zip.File, onDuplicate string) {
	// 收集子游戏顶层目录（保持 zip 内出现顺序）
	order := make([]string, 0)
	seen := make(map[string]bool)
	for _, f := range files {
		name := strings.TrimPrefix(f.Name, "./")
		idx := strings.Index(name, "/")
		if idx <= 0 {
			continue
		}
		top := name[:idx] // 如 "13"
		if isDigits(top) && !seen[top] {
			seen[top] = true
			order = append(order, top)
		}
	}

	type childResult struct {
		ID       string       `json:"index"`
		Name     string       `json:"name"`
		Status   string       `json:"status"` // ok | overwritten | skipped | error
		Message  string       `json:"message,omitempty"`
		Reason   string       `json:"reason,omitempty"`
		Game     *models.Game `json:"game,omitempty"`
		Existing *models.Game `json:"existing,omitempty"`
	}
	results := make([]childResult, 0, len(order))
	ok, overwritten, skipped, aliased := 0, 0, 0, 0

	for _, id := range order {
		prefix := id + "/"
		child := extractChildEntries(files, prefix)

		// 解析子游戏的 config.json
		var cfg *GameConfig
		for _, f := range child {
			if strings.TrimPrefix(f.Name, "./") == prefix+"config.json" {
				if gc, ok2 := decodeGameConfig(f); ok2 {
					cfg = gc
				}
				break
			}
		}
		if cfg == nil {
			results = append(results, childResult{ID: id, Status: "error", Message: "子游戏 config.json 缺失或解析失败"})
			continue
		}

		// 指盘子游戏文件指纹：剥离 "<id>/files/"，与单游戏包保持一致
		strip := prefix + "files/"
		hash := zipContentHashWithPrefix(child, strip)
		childPlatformTag := platformTagFromZip(child)

		// 逐游戏去重（仅按内容指纹）
		if dup := findDuplicate(hash, cfg.Name); dup != nil {
			existing := dup.Existing
			// 指纹相同但名称不同 → 不视为重复，把新名称追加为「别名」
			if normalizeName(existing.Name) != normalizeName(cfg.Name) {
				if list, added := appendAlias(existing, cfg.Name); added {
					_ = database.DB.Model(&models.Game{}).Where("id = ?", existing.ID).
						Update("aliases", encodeAliases(list)).Error
					existing.Aliases = encodeAliases(list)
					aliased++
					results = append(results, childResult{
						ID: id, Name: cfg.Name, Status: "alias_added", Reason: dup.Reason,
						Message:  fmt.Sprintf("已追加别名（属于「%s」）", existing.Name),
						Existing: existing,
					})
					continue
				}
				// 别名已存在（同名归一化）→ 视为重复，按 skip/overwrite 处理
			}
			if onDuplicate == "skip" {
				skipped++
				results = append(results, childResult{
					ID: id, Name: cfg.Name, Status: "skipped", Reason: dup.Reason,
					Message:  fmt.Sprintf("已跳过重复（与「%s」重复）", existing.Name),
					Existing: existing,
				})
				continue
			}
			// overwrite：解包覆盖到既有 mount_path
			dir, derr := unpackChildEntries(child, existing.MountPath, strip)
			if derr != nil {
				results = append(results, childResult{ID: id, Name: cfg.Name, Status: "error", Message: "解包失败"})
				continue
			}
			updates := map[string]interface{}{
				"name":         cfg.Name,
				"cover_path":   cfg.CoverPath,
				"mount_path":   dir,
				"tag":          cfg.Tag,
				"content_hash": hash,
				"size_bytes":   computeGameSize(dir),
			}
			// 封面处理：data URI 解码落盘 / 包内候选图提取
			if cover := resolveImportedCover(cfg, child, dir, strip); cover != "" {
				updates["cover_path"] = cover
			}
			if childPlatformTag != "" {
				updates["platform_tag"] = childPlatformTag
			}
			if cfg.SortOrder != 0 {
				updates["sort_order"] = cfg.SortOrder
			}
			if cfg.IsLandscape != 0 {
				updates["is_landscape"] = cfg.IsLandscape
			}
			if uerr := database.DB.Model(&models.Game{}).Where("id = ?", existing.ID).Updates(updates).Error; uerr != nil {
				results = append(results, childResult{ID: id, Name: cfg.Name, Status: "error", Message: "更新数据库失败"})
				continue
			}
			batchCats := resolveImportCategoryIDs(cfg)
			saveGameRelations(existing.ID, batchCats, cfg.ResolvedTags())
			var out models.Game
			_ = database.DB.First(&out, existing.ID).Error
			out.CategoryIDs = batchCats
			out.Tags = cfg.ResolvedTags()
			overwritten++
			results = append(results, childResult{
				ID: id, Name: cfg.Name, Status: "overwritten", Reason: dup.Reason,
				Message: fmt.Sprintf("已覆盖（原「%s」）", existing.Name), Game: &out,
			})
			continue
		}

		// 非重复：解包 + 建库（重新分配 ID）
		game, gerr := buildGameFromConfig(cfg, hash)
		if gerr != nil {
			results = append(results, childResult{ID: id, Name: cfg.Name, Status: "error", Message: "构造记录失败"})
			continue
		}
		dir, derr := unpackChildEntries(child, "", strip)
		if derr != nil {
			results = append(results, childResult{ID: id, Name: cfg.Name, Status: "error", Message: "解包失败"})
			continue
		}
		game.MountPath = dir
		game.PlatformTag = childPlatformTag
		game.SizeBytes = computeGameSize(dir)
		// 封面处理：data URI 解码落盘 / 包内候选图提取
		if cover := resolveImportedCover(cfg, child, dir, strip); cover != "" {
			game.CoverPath = cover
		}
		catIDs := resolveImportCategoryIDs(cfg)
		tags := cfg.ResolvedTags()
		if len(catIDs) == 0 {
			if catID := autoClassifyGame(game.Name, strings.Join(tags, " ")); catID != 0 {
				game.CategoryID = catID
				catIDs = []uint{catID}
			}
		}
		if cerr := database.DB.Create(&game).Error; cerr != nil {
			results = append(results, childResult{ID: id, Name: cfg.Name, Status: "error", Message: "写库失败"})
			continue
		}
		saveGameRelations(game.ID, catIDs, tags)
		game.CategoryIDs = catIDs
		if game.CategoryIDs == nil {
			game.CategoryIDs = []uint{}
		}
		game.Tags = tags
		if game.Tags == nil {
			game.Tags = []string{}
		}
		ok++
		results = append(results, childResult{ID: id, Name: cfg.Name, Status: "ok", Game: &game})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("批量导入完成：成功 %d，覆盖 %d，跳过 %d，别名 %d，共 %d", ok, overwritten, skipped, aliased, len(order)),
		"batch":   true,
		"total":   len(order),
		"aliased": aliased,
		"results": results,
	})
}

// unpackChildEntries 解压子游戏条目到目标目录
//   - explicitDir 非空：覆盖模式，直接复用既有 mount_path
//   - 否则：落到 gamesDir()/<sanitizeDirName(name)>（name 取自条目内 config.json）
//
// strip 为需剥离的前缀（"<id>/files/"）
func unpackChildEntries(child []*zip.File, explicitDir, strip string) (string, error) {
	// 子游戏自身的 config.json 路径恰为 strip 的上一级：<prefix>config.json
	// 例如 strip="1/files/" → selfCfg="1/config.json"
	selfCfg := strings.TrimSuffix(strip, "files/") + "config.json"
	var cfgName string
	for _, f := range child {
		if strings.TrimPrefix(f.Name, "./") == selfCfg {
			if gc, ok := decodeGameConfig(f); ok {
				cfgName = gc.Name
			}
			break
		}
	}
	var dest string
	if explicitDir != "" {
		dest = explicitDir
	} else {
		dirName := sanitizeDirName(cfgName)
		if dirName == "" {
			dirName = "game"
		}
		dest = filepath.Join(gamesDir(), dirName)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	for _, f := range child {
		if strings.TrimPrefix(f.Name, "./") == selfCfg {
			continue // 跳过子游戏自身的 config.json（游戏内各 bundle 的 config.json 必须保留）
		}
		if err := extractZipEntry(f, dest, strip); err != nil {
			return "", err
		}
	}
	return dest, nil
}

// gamesDir 返回游戏文件根目录（并尽力创建）
func gamesDir() string {
	dir := ""
	if Cfg != nil {
		dir = Cfg.GamesDir
	}
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "uogame-games")
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// sanitizeDirName 将游戏名转为安全的目录名（去除路径分隔符等非法字符）
func sanitizeDirName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "game"
	}
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_",
	)
	name = replacer.Replace(name)
	return strings.Trim(name, ". ")
}

// unpackGame 解压 zip 内除 config.json 外的全部文件到游戏存储目录
// 目录命名：{名称}（冲突时追加 -{id}）；返回解压目录绝对路径
//
// 兼容两种包结构（原版导出格式 vs 直接打包）：
//  1. config.json + game_files/...   → 剥掉 game_files/ 前缀，内容提升到游戏目录根
//  2. config.json + index.html/...   → 原样解压
//
// 判定依据：若所有非 config 条目都位于同一顶层目录下（如 game_files/），则剥离该层。
func unpackGame(files []*zip.File, name string, existingID uint, explicitDir string) (string, error) {
	var dest string
	if explicitDir != "" {
		// 覆盖模式：直接复用既有 mount_path，避免生成 "-{id}" 新目录
		dest = explicitDir
	} else {
		base := gamesDir()
		dirName := sanitizeDirName(name)
		if dirName == "" {
			dirName = "game"
		}
		sub := dirName
		if existingID > 0 {
			sub = fmt.Sprintf("%s-%d", dirName, existingID)
		}
		dest = filepath.Join(base, sub)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	stripPrefix := detectCommonPrefix(files)
	for _, f := range files {
		if f.Name == "config.json" {
			continue
		}
		if err := extractZipEntry(f, dest, stripPrefix); err != nil {
			return "", err
		}
	}
	return dest, nil
}

// detectCommonPrefix 探测所有非 config 条目是否共用同一个顶层目录（如 "game_files/"）
// 命中则返回该前缀（含结尾分隔符），否则返回空串
// 同时兼容 zip 条目使用 "/" 或 "\" 分隔符（部分打包工具在 Windows 下写 "\"）
func detectCommonPrefix(files []*zip.File) string {
	prefix := ""
	for _, f := range files {
		if f.Name == "config.json" {
			continue
		}
		name := strings.TrimPrefix(f.Name, "./")
		idx := strings.IndexAny(name, "/\\")
		if idx <= 0 {
			// 顶层存在散落文件 → 无统一前缀
			return ""
		}
		top := name[:idx+1]
		if prefix == "" {
			prefix = top
		} else if prefix != top {
			return ""
		}
	}
	return prefix
}

// extractZipEntry 解压单个 zip 条目到 dest（防路径穿越）；stripPrefix 非空时剥离该前缀
func extractZipEntry(f *zip.File, dest, stripPrefix string) error {
	name := strings.TrimPrefix(f.Name, "./")
	if stripPrefix != "" && strings.HasPrefix(name, stripPrefix) {
		name = strings.TrimPrefix(name, stripPrefix)
		if name == "" {
			return nil // 前缀目录本身，跳过
		}
	}
	// 统一分隔符，防止 "game_files\a.js" 被当作单段文件名
	name = strings.ReplaceAll(name, "\\", "/")
	target := filepath.Join(dest, filepath.Clean("/"+name))
	if !strings.HasPrefix(target, dest) {
		return fmt.Errorf("zip entry out of range: %s", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// saveUploadTemp 将上传文件保存到临时文件，返回临时路径
func saveUploadTemp(fileHeader *multipart.FileHeader) (string, error) {
	tmp, err := os.CreateTemp("", "import-*.zip")
	if err != nil {
		return "", err
	}
	src, oerr := fileHeader.Open()
	if oerr != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", oerr
	}
	defer src.Close()
	if _, cerr := io.Copy(tmp, src); cerr != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", cerr
	}
	tmp.Close()
	return tmp.Name(), nil
}

// removeTemp 删除临时文件
func removeTemp(path string) {
	_ = os.Remove(path)
}

// decodeGameConfig 解析 zip 内 config.json 条目
// 兼容 Windows 工具（PowerShell/Python）生成的 UTF-8 BOM 头，自动剥离
func decodeGameConfig(f *zip.File) (*GameConfig, bool) {
	rc, err := f.Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, false
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // 剥离 UTF-8 BOM
	var gc GameConfig
	if json.Unmarshal(data, &gc) != nil {
		return nil, false
	}
	return &gc, true
}

// embedCoverForExport 导出时把封面写入 zip 并返回可移植的相对路径
//   - cover_path 为 data URI：解码后以 cover.<ext> 写入 zip
//   - cover_path 为可读文件：以 cover.<ext> 写入 zip
//   - 其他/空：返回空串，不写封面
//
// zipPrefix 为多游戏包时的子目录前缀（如 "13/"，单游戏传 ""）
func embedCoverForExport(zw *zip.Writer, coverPath, zipPrefix string) string {
	cp := strings.TrimSpace(coverPath)
	if cp == "" {
		return ""
	}
	var data []byte
	ext := ".png"
	if isImageDataURI(cp) {
		e, d, ok := decodeImageDataURI(cp)
		if !ok {
			return ""
		}
		ext, data = e, d
	} else {
		p := resolveGamePath(cp)
		b, err := os.ReadFile(p)
		if err != nil {
			return ""
		}
		if e := strings.ToLower(filepath.Ext(p)); e != "" {
			ext = e
		}
		data = b
	}
	name := zipPrefix + "cover" + ext
	w, err := zw.Create(name)
	if err != nil {
		return ""
	}
	if _, err := w.Write(data); err != nil {
		return ""
	}
	return "cover" + ext
}

// resolveGamePath 将 mount_path（物理绝对路径）规整为本地路径
func resolveGamePath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// maxCoverBytes 封面 base64 解码后的大小上限（5MB），防止内存放大
const maxCoverBytes = 5 << 20

// dataURIPrefix 判断字符串是否为图片 data URI
func isImageDataURI(s string) bool {
	return strings.HasPrefix(s, "data:image/")
}

// decodeImageDataURI 解析 data:image/<sub>;base64,<payload>
// 返回推断出的扩展名（含点，如 ".png"）与解码后的字节；失败返回 ok=false
func decodeImageDataURI(s string) (ext string, data []byte, ok bool) {
	if !isImageDataURI(s) {
		return "", nil, false
	}
	comma := strings.Index(s, ",")
	if comma < 0 {
		return "", nil, false
	}
	header := s[len("data:"):comma] // 形如 image/png;base64
	payload := s[comma+1:]
	if !strings.Contains(header, "base64") {
		return "", nil, false
	}
	mime := header
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = mime[:idx]
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 || len(raw) > maxCoverBytes {
		return "", nil, false
	}
	return imageExtFor(mime), raw, true
}

// imageExtFor 依 mime 推断图片扩展名（默认 .png）
func imageExtFor(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}

// saveCoverDataURI 将 data URI 封面解码并写入游戏目录（cover.<ext>）
// 返回写入后的绝对路径；失败返回 ok=false
func saveCoverDataURI(gameDir, dataURI string) (string, bool) {
	if gameDir == "" {
		return "", false
	}
	ext, data, ok := decodeImageDataURI(dataURI)
	if !ok {
		return "", false
	}
	dst := filepath.Join(gameDir, "cover"+ext)
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return "", false
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", false
	}
	return dst, true
}

// imageExts 包内提取封面时认可的图片扩展名
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
}

// coverCandidateScore 包内图片作为封面的优先级评分（越小越优先）；-1 表示不候选
// 优先级：cover.* > icon/logo.* > screenshot/thumb.* > 其他图片
func coverCandidateScore(rel string) int {
	lower := strings.ToLower(rel)
	ext := strings.ToLower(filepath.Ext(lower))
	if !imageExts[ext] {
		return -1
	}
	// 跳过明显的杂项资源目录（图集/切片），降低误选
	base := strings.ToLower(filepath.Base(lower))
	switch {
	case strings.HasPrefix(base, "cover"):
		return 0
	case strings.HasPrefix(base, "icon"), strings.HasPrefix(base, "logo"):
		return 1
	case strings.HasPrefix(base, "screenshot"), strings.HasPrefix(base, "thumb"),
		strings.HasPrefix(base, "preview"), strings.HasPrefix(base, "poster"):
		return 2
	default:
		return 3
	}
}

// extractPackageCover 从 zip 条目中挑选一张候选图片作为封面并解压到 dest
// 返回解压后的绝对路径；无候选或失败返回 ok=false
// stripPrefix 需与文件解包保持一致（剥离公共前缀）
func extractPackageCover(files []*zip.File, dest, stripPrefix string) (string, bool) {
	bestScore := 99
	var bestFile *zip.File
	var bestName string
	for _, f := range files {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(f.Name, "./")
		if stripPrefix != "" && strings.HasPrefix(name, stripPrefix) {
			name = strings.TrimPrefix(name, stripPrefix)
		}
		name = strings.ReplaceAll(name, "\\", "/")
		if name == "" || strings.EqualFold(name, "config.json") {
			continue
		}
		score := coverCandidateScore(name)
		if score < 0 {
			continue
		}
		// 过滤过小的图片（疑似图标切片）
		if f.UncompressedSize64 > 0 && f.UncompressedSize64 < 2048 {
			continue
		}
		if score < bestScore {
			bestScore = score
			bestFile = f
			bestName = name
		}
	}
	if bestFile == nil {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(bestName))
	dst := filepath.Join(dest, "cover"+ext)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", false
	}
	rc, err := bestFile.Open()
	if err != nil {
		return "", false
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", false
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		return "", false
	}
	return dst, true
}

// resolveImportedCover 统一处理导入时的封面：优先 data URI 落盘，其次包内提取
// 返回封面绝对路径（可能为空表示无封面）
func resolveImportedCover(cfg *GameConfig, files []*zip.File, gameDir, stripPrefix string) string {
	if cfg == nil || gameDir == "" {
		return ""
	}
	cp := strings.TrimSpace(cfg.CoverPath)
	// 1) data URI：解码落盘为 cover.<ext>
	if isImageDataURI(cp) {
		if p, ok := saveCoverDataURI(gameDir, cp); ok {
			return p
		}
		// data URI 落盘失败则继续尝试包内提取
		return firstPackageCover(files, gameDir, stripPrefix)
	}
	// 2) 相对路径：解析为游戏目录下的绝对路径（保证跨机可移植）
	if cp != "" && !filepath.IsAbs(cp) {
		abs := filepath.Join(gameDir, filepath.FromSlash(cp))
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
		// 相对路径指向的文件不存在：回退包内候选图
		return firstPackageCover(files, gameDir, stripPrefix)
	}
	// 3) 绝对路径：沿用原值（视为可访问路径）
	if cp != "" {
		return cp
	}
	// 4) 无封面：从包内挑选候选图
	return firstPackageCover(files, gameDir, stripPrefix)
}

// firstPackageCover 从包内提取候选封面，失败返回空串
func firstPackageCover(files []*zip.File, gameDir, stripPrefix string) string {
	if p, ok := extractPackageCover(files, gameDir, stripPrefix); ok {
		return p
	}
	return ""
}

// writeZipJSON 向 zip 写入 JSON 文件
func writeZipJSON(zw *zip.Writer, name string, v interface{}) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(v)
}

// writeZipFile 向 zip 写入单个文件
func writeZipFile(zw *zip.Writer, path, name string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

// writeZipDir 递归写入目录到 zip
func writeZipDir(zw *zip.Writer, base, prefix string) error {
	return filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(base, path)
		if rerr != nil {
			return rerr
		}
		return writeZipFile(zw, path, prefix+"/"+filepath.ToSlash(rel))
	})
}
