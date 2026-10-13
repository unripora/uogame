package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// PlayTimeRank 时长榜条目
type PlayTimeRank struct {
	Nickname string  `json:"nickname"`
	PlayTime float64 `json:"playTime"`
}

// HotGameRank 热门榜条目
type HotGameRank struct {
	Name           string  `json:"name"`
	Score          float64 `json:"score"`
	Players        int     `json:"players"`
	CollectionName string  `json:"collection_name"`
}

// GetPlayTimeLeaderboard 时长榜：按用户 total_play_time 降序
// 前端约定：返回**裸数组**，字段 {nickname, playTime}
func GetPlayTimeLeaderboard(c *gin.Context) {
	var users []models.User
	if err := database.DB.Where("uid <> '' AND total_play_time > 0").Order("total_play_time desc").Limit(50).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch leaderboard"})
		return
	}
	ranks := make([]PlayTimeRank, 0, len(users))
	for _, u := range users {
		ranks = append(ranks, PlayTimeRank{
			Nickname: displayName(u),
			PlayTime: u.TotalPlayTime,
		})
	}
	c.JSON(http.StatusOK, ranks)
}

// GetHotGamesLeaderboard 热门榜：按游戏 hot_time 降序
// 前端约定：返回**裸数组**，字段 {name, players, score, collection_name}
func GetHotGamesLeaderboard(c *gin.Context) {
	var games []models.Game
	if err := database.DB.Order("hot_time desc").Limit(50).Find(&games).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch leaderboard"})
		return
	}
	ranks := make([]HotGameRank, 0, len(games))
	for _, g := range games {
		ranks = append(ranks, HotGameRank{
			Name:           g.Name,
			Score:          g.HotTime,
			Players:        0, // 去重游玩人数在当前数据模型下未单独持久化
			CollectionName: collectionName(g.CategoryID),
		})
	}
	c.JSON(http.StatusOK, ranks)
}

// ResetLeaderboards 重置排行榜（清零用户时长与游戏热度）
func ResetLeaderboards(c *gin.Context) {
	database.DB.Model(&models.User{}).Where("1 = 1").Update("total_play_time", 0)
	database.DB.Model(&models.Game{}).Where("1 = 1").Update("hot_time", 0)
	c.JSON(http.StatusOK, gin.H{"message": "排行榜已重置"})
}

// displayName 返回用户展示名：昵称优先，其次用户名，最后 "游客"
func displayName(u models.User) string {
	if u.Nickname != "" {
		return u.Nickname
	}
	if u.Username != "" {
		return u.Username
	}
	return "游客"
}

// collectionName 根据分类 ID 返回分类名称（用于热门榜合集标签）
func collectionName(categoryID uint) string {
	if categoryID == 0 {
		return ""
	}
	var cat models.Category
	if err := database.DB.First(&cat, categoryID).Error; err != nil {
		return ""
	}
	return cat.Name
}
