package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// 游戏评分
// 设计：
//   - game_ratings 明细表，一人一票（rater_key = 账号编码，跨玩家/管理员体系唯一），可修改；
//   - 综合评分以缓存字段落库（games.avg_rating / rating_count），写评分时聚合更新，
//     避免列表接口实时 AVG 聚合。

const (
	ratingMin = 1
	ratingMax = 5
)

// raterKey 取当前登录账号的编码（玩家 0000001 / 管理员 G00）
// 兜底：无编码时用 userID 前缀区分，避免跨体系 ID 冲突
func raterKey(c *gin.Context) string {
	if code := strings.TrimSpace(c.GetString("accountCode")); code != "" {
		return code
	}
	if id, ok := c.Get("userID"); ok {
		if v, ok2 := id.(uint); ok2 && v > 0 {
			return "u" + strconv.FormatUint(uint64(v), 10)
		}
	}
	return ""
}

// recalcGameRating 依据明细重算并回写某游戏的综合评分缓存
func recalcGameRating(gameID uint) (float64, int, error) {
	var count int64
	if err := database.DB.Model(&models.GameRating{}).
		Where("game_id = ?", gameID).Count(&count).Error; err != nil {
		return 0, 0, err
	}
	var avg float64
	if count > 0 {
		var sum int64
		if err := database.DB.Model(&models.GameRating{}).
			Where("game_id = ?", gameID).
			Select("COALESCE(SUM(score), 0)").Scan(&sum).Error; err != nil {
			return 0, 0, err
		}
		avg = float64(sum) / float64(count)
	}
	if err := database.DB.Model(&models.Game{}).Where("id = ?", gameID).
		Updates(map[string]any{"avg_rating": avg, "rating_count": count}).Error; err != nil {
		return 0, 0, err
	}
	return avg, int(count), nil
}

// RateGame 提交/修改当前账号对某游戏的评分
// POST /api/games/:id/rating  body: {score: 1..5}
func RateGame(c *gin.Context) {
	gameID, _ := strconv.Atoi(c.Param("id"))
	if gameID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid game id"})
		return
	}
	var payload struct {
		Score int `json:"score"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Score < ratingMin || payload.Score > ratingMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评分需为 1~5 星"})
		return
	}
	var game models.Game
	if err := database.DB.First(&game, gameID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	key := raterKey(c)
	if key == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未识别的账号"})
		return
	}

	now := time.Now().Unix()
	var exist models.GameRating
	err := database.DB.Where("game_id = ? AND rater_key = ?", gameID, key).First(&exist).Error
	switch {
	case err == gorm.ErrRecordNotFound:
		row := models.GameRating{
			GameID: uint(gameID), RaterKey: key,
			Score: payload.Score, CreatedAt: now, UpdatedAt: now,
		}
		if cerr := database.DB.Create(&row).Error; cerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save rating"})
			return
		}
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save rating"})
		return
	default:
		if uerr := database.DB.Model(&models.GameRating{}).Where("id = ?", exist.ID).
			Updates(map[string]any{"score": payload.Score, "updated_at": now}).Error; uerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save rating"})
			return
		}
	}

	avg, count, rerr := recalcGameRating(uint(gameID))
	if rerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update rating"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":      "评分成功",
		"my_score":     payload.Score,
		"avg_rating":   avg,
		"rating_count": count,
	})
}

// UnrateGame 取消当前账号对某游戏的评分
// DELETE /api/games/:id/rating
func UnrateGame(c *gin.Context) {
	gameID, _ := strconv.Atoi(c.Param("id"))
	if gameID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid game id"})
		return
	}
	key := raterKey(c)
	if key == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未识别的账号"})
		return
	}
	if err := database.DB.Where("game_id = ? AND rater_key = ?", gameID, key).
		Delete(&models.GameRating{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete rating"})
		return
	}
	avg, count, rerr := recalcGameRating(uint(gameID))
	if rerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update rating"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":      "已取消评分",
		"my_score":     0,
		"avg_rating":   avg,
		"rating_count": count,
	})
}

// ListMyRatings 返回当前账号对所有游戏的评分（game_id 字符串 → 分数）
// GET /api/ratings/mine
func ListMyRatings(c *gin.Context) {
	out := map[string]int{}
	key := raterKey(c)
	if key == "" {
		c.JSON(http.StatusOK, out)
		return
	}
	var rows []models.GameRating
	database.DB.Where("rater_key = ?", key).Find(&rows)
	for _, r := range rows {
		out[strconv.FormatUint(uint64(r.GameID), 10)] = r.Score
	}
	c.JSON(http.StatusOK, out)
}