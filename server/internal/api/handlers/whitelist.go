package handlers

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// 白名单分类功能
// 设计：
//   - 词库存 settings 表，key = "whitelist_categories"
//   - 5 大类默认词库在首次读取时自动播种，并确保分类表中存在同名分类
//   - 匹配规则：标签精确命中优先；否则游戏 name（含 tag）包含关键词即命中；
//     多类命中取命中数最多者，相同按 sort_order 升序

const whitelistSettingKey = "whitelist_categories"

// WhitelistGroup 一个分类的词库配置
// Keywords：关键词（对「游戏名 + 标签」做包含匹配）
// Tags：标签精确规则（游戏带该标签即命中，优先于关键词）
type WhitelistGroup struct {
	CategoryID   uint     `json:"category_id"`
	CategoryName string   `json:"category_name"`
	Keywords     []string `json:"keywords"`
	Tags         []string `json:"tags"`
}

// defaultWhitelist 5 大类默认词库（来源：用户拟定白名单分类设置）
func defaultWhitelist() []WhitelistGroup {
	return []WhitelistGroup{
		{
			CategoryName: "休闲解压",
			Keywords: []string{
				"俄罗斯方块", "泡泡龙", "淘金者", "打砖块", "马戏团",
				"瓦里奥", "星之卡比", "通勤一笔",
				"羊了个羊", "躺平", "别惹", "抓大鹅", "脑洞大师", "斗地主",
				"消除", "合成", "找茬", "解谜", "放置",
			},
		},
		{
			CategoryName: "动作对抗",
			Keywords: []string{
				"魂斗罗", "双截龙", "赤色要塞", "绿色兵团", "影子传说",
				"恶魔城", "银河战士", "索尼克", "真三国无双",
				"全民枪神", "纸片大混战", "双人决斗", "狙击", "水世界",
				"射击", "跑酷", "闯关", "乱斗", "大作战",
			},
		},
		{
			CategoryName: "策略养成",
			Keywords: []string{
				"三国志", "机器人大战", "霸王的大陆", "圣火徽章",
				"高级战争", "火焰纹章", "口袋妖怪", "最终幻想战略版",
				"生存33天", "传奇之业", "三国大冒险", "最强斗王", "蔚蓝星球",
				"塔防", "放置挂机", "卡牌", "霸业",
			},
		},
		{
			CategoryName: "模拟经营",
			Keywords: []string{
				"牧场物语", "模拟城市", "大富翁", "模拟人生", "主题公园",
				"我的花园", "向往的生活", "暴吵萌厨", "黑洞降临",
				"经营", "建造", "小镇", "花园",
			},
		},
		{
			CategoryName: "竞技棋牌",
			Keywords: []string{
				"热血足球", "热血篮球", "火箭车", "中国象棋", "五子棋",
				"马里奥赛车", "F-ZERO", "网球王子", "实况足球",
				"台球", "桌球", "极品飞车", "麻将", "掼蛋", "军棋",
				"棋", "赛车",
			},
		},
	}
}

// ensureWhitelistDefaults 确保白名单设置存在，并同步分类表
// 返回当前的词库配置（已回填 category_id）
func ensureWhitelistDefaults() ([]WhitelistGroup, error) {
	groups := defaultWhitelist()

	var setting models.Setting
	err := database.DB.First(&setting, "key = ?", whitelistSettingKey).Error
	if err == nil && strings.TrimSpace(setting.Value) != "" {
		// 已有配置：解析
		var saved []WhitelistGroup
		if json.Unmarshal([]byte(setting.Value), &saved) == nil && len(saved) > 0 {
			groups = saved
		}
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// 同步分类表：缺哪个建哪个，并回填 category_id
	for i := range groups {
		catID, cerr := ensureCategory(groups[i].CategoryName, i+1)
		if cerr != nil {
			return nil, cerr
		}
		groups[i].CategoryID = catID
	}

	// 回写（补齐 category_id）
	if b, merr := json.Marshal(groups); merr == nil {
		_ = database.DB.Save(&models.Setting{Key: whitelistSettingKey, Value: string(b)}).Error
	}
	return groups, nil
}

// ensureCategory 按名称查找分类，不存在则创建
func ensureCategory(name string, sortOrder int) (uint, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	var cat models.Category
	err := database.DB.First(&cat, "name = ?", name).Error
	if err == nil {
		return cat.ID, nil
	}
	if err != gorm.ErrRecordNotFound {
		return 0, err
	}
	cat = models.Category{Name: name, SortOrder: sortOrder}
	if cerr := database.DB.Create(&cat).Error; cerr != nil {
		return 0, cerr
	}
	return cat.ID, nil
}

// GetWhitelist 返回白名单配置（含 5 分类与 category_id）
// GET /api/admin/whitelist
func GetWhitelist(c *gin.Context) {
	groups, err := ensureWhitelistDefaults()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load whitelist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"categories": groups})
}

// UpdateWhitelist 保存白名单配置
// PUT /api/admin/whitelist  body: {categories:[{category_id,category_name,keywords}]}
func UpdateWhitelist(c *gin.Context) {
	var payload struct {
		Categories []WhitelistGroup `json:"categories"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	// 规范化：去空、去重
	for i := range payload.Categories {
		payload.Categories[i].CategoryName = strings.TrimSpace(payload.Categories[i].CategoryName)
		payload.Categories[i].Keywords = normalizeKeywords(payload.Categories[i].Keywords)
		payload.Categories[i].Tags = normalizeKeywords(payload.Categories[i].Tags)
		// 确保分类存在
		if payload.Categories[i].CategoryName != "" {
			catID, err := ensureCategory(payload.Categories[i].CategoryName, i+1)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to ensure category"})
				return
			}
			payload.Categories[i].CategoryID = catID
		}
	}
	b, err := json.Marshal(payload.Categories)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode whitelist"})
		return
	}
	if err := database.DB.Save(&models.Setting{Key: whitelistSettingKey, Value: string(b)}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save whitelist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "白名单已保存", "categories": payload.Categories})
}

// ClassifyGames 按白名单批量识别并归类
// POST /api/games/classify  body(可选): {ids?:[]}  不传则处理全部未分类游戏
func ClassifyGames(c *gin.Context) {
	var payload struct {
		IDs []uint `json:"ids"`
	}
	_ = c.ShouldBindJSON(&payload)

	groups, err := ensureWhitelistDefaults()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load whitelist"})
		return
	}

	query := database.DB.Model(&models.Game{})
	if len(payload.IDs) > 0 {
		query = query.Where("id IN ?", payload.IDs)
	} else {
		// 未指定：仅处理未分类（category_id = 0）
		query = query.Where("category_id = 0 OR category_id IS NULL")
	}
	var games []models.Game
	if err := query.Find(&games).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch games"})
		return
	}

	updated := make([]gin.H, 0, len(games))
	for i := range games {
		// 匹配源 = 游戏名 + 标签（多值）：标签走精确规则，关键词走包含匹配
		loadGameRelations(games)
		catID, matched := matchCategory(games[i].Name, games[i].Tags, groups)
		if catID == 0 {
			continue
		}
		// 追加分类关联（多分类叠加，不覆盖）
		addGameCategory(games[i].ID, catID)
		updated = append(updated, gin.H{
			"id":          games[i].ID,
			"category_id": catID,
			"matched":     matched,
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": "白名单识别完成", "updated": updated})
}

// UndoClassify 撤回白名单分类（将指定游戏或全部已分类）恢复为默认分类
// POST /api/games/classify/undo  body: {ids?:[]}
func UndoClassify(c *gin.Context) {
	var payload struct {
		IDs []uint `json:"ids"`
	}
	_ = c.ShouldBindJSON(&payload)

	groups, err := ensureWhitelistDefaults()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load whitelist"})
		return
	}
	// 收集白名单分类 ID
	catIDs := make([]uint, 0, len(groups))
	for _, g := range groups {
		if g.CategoryID != 0 {
			catIDs = append(catIDs, g.CategoryID)
		}
	}
	if len(catIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "无可撤回分类", "updated": []gin.H{}})
		return
	}

	// 定位含有白名单分类关联的游戏（多分类场景：仅移除白名单来源的分类）
	q := database.DB.Model(&models.GameCategory{}).Where("category_id IN ?", catIDs)
	if len(payload.IDs) > 0 {
		q = q.Where("game_id IN ?", payload.IDs)
	}
	var rels []models.GameCategory
	if err := q.Find(&rels).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch games"})
		return
	}

	updated := make([]gin.H, 0, len(rels))
	for _, r := range rels {
		removeGameCategories(r.GameID, []uint{r.CategoryID})
		updated = append(updated, gin.H{"id": r.GameID, "category_id": r.CategoryID})
	}
	c.JSON(http.StatusOK, gin.H{"message": "已撤回分类", "updated": updated})
}

// matchCategory 按标签与关键词匹配，返回命中的分类 ID 与命中详情
// 规则：
//  1. 标签精确命中（游戏标签 ∈ 该组 tags）优先于关键词包含匹配
//  2. 同级比命中数，多者胜；数量相同按配置顺序（切片下标）升序
//  3. 关键词仍对「游戏名 + 标签文本」做包含匹配（保持原语义）
func matchCategory(name string, tags []string, groups []WhitelistGroup) (uint, []string) {
	lowerName := strings.ToLower(name)
	// 关键词匹配文本 = 游戏名 + 标签（与旧行为一致）
	kwText := lowerName
	if len(tags) > 0 {
		kwText += " " + strings.ToLower(strings.Join(tags, " "))
	}
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			tagSet[t] = true
		}
	}

	type score struct {
		catID   uint
		order   int
		tagHits int
		kwHits  int
		matched []string
	}
	var scores []score
	for idx, g := range groups {
		var matched []string
		tagHits := 0
		for _, t := range g.Tags {
			if t = strings.TrimSpace(t); t != "" && tagSet[strings.ToLower(t)] {
				tagHits++
				matched = append(matched, "标签:"+t)
			}
		}
		kwHits := 0
		for _, kw := range g.Keywords {
			if kw == "" {
				continue
			}
			if strings.Contains(kwText, strings.ToLower(kw)) {
				kwHits++
				matched = append(matched, kw)
			}
		}
		if tagHits > 0 || kwHits > 0 {
			scores = append(scores, score{
				catID: g.CategoryID, order: idx,
				tagHits: tagHits, kwHits: kwHits, matched: matched,
			})
		}
	}
	if len(scores) == 0 {
		return 0, nil
	}
	sort.SliceStable(scores, func(i, j int) bool {
		si, sj := scores[i], scores[j]
		// 1) 有标签精确命中者优先
		if (si.tagHits > 0) != (sj.tagHits > 0) {
			return si.tagHits > 0
		}
		// 2) 标签命中数多者优先
		if si.tagHits != sj.tagHits {
			return si.tagHits > sj.tagHits
		}
		// 3) 关键词命中数多者优先
		if si.kwHits != sj.kwHits {
			return si.kwHits > sj.kwHits
		}
		// 4) 配置顺序
		return si.order < sj.order
	})
	return scores[0].catID, scores[0].matched
}

// normalizeKeywords 关键词去空、去重、去首尾空白
func normalizeKeywords(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, k := range in {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// autoClassifyGame 导入时调用：按白名单匹配单个游戏名 + 标签，返回分类 ID
func autoClassifyGame(name, tag string) uint {
	groups, err := ensureWhitelistDefaults()
	if err != nil {
		return 0
	}
	var tags []string
	if strings.TrimSpace(tag) != "" {
		tags = append(tags, tag)
	}
	catID, _ := matchCategory(name, tags, groups)
	return catID
}

// BatchUpdateGameCategory 批量修改游戏分类
// POST /api/games/batch-category  body: {ids:[], category_id} 或 {ids:[], category_ids:[]}
// 语义：覆盖式设置分类（多分类场景下重写该批游戏的分类关联）
func BatchUpdateGameCategory(c *gin.Context) {
	var payload struct {
		IDs         []uint `json:"ids"`
		CategoryID  uint   `json:"category_id"`
		CategoryIDs []uint `json:"category_ids"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if len(payload.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No games selected"})
		return
	}
	// 目标分类：优先数组，回退单值
	catIDs := payload.CategoryIDs
	if len(catIDs) == 0 && payload.CategoryID > 0 {
		catIDs = []uint{payload.CategoryID}
	}
	catIDs = normalizeIDs(catIDs)

	for _, gid := range payload.IDs {
		// 保留原标签，仅重写分类
		var gts []models.GameTag
		database.DB.Where("game_id = ?", gid).Find(&gts)
		tags := make([]string, 0, len(gts))
		for _, t := range gts {
			tags = append(tags, t.Tag)
		}
		saveGameRelations(gid, catIDs, tags)
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量分类修改成功"})
}

// BatchDeleteGames 批量删除游戏
// POST /api/games/batch-delete  body: {ids:[]}
func BatchDeleteGames(c *gin.Context) {
	var payload struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if len(payload.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No games selected"})
		return
	}
	if err := database.DB.Where("id IN ?", payload.IDs).Delete(&models.Game{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete games"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量删除成功"})
}
