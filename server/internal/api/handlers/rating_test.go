package handlers

import (
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// gameParam 构造 :id 路由参数
func gameParam(id uint) gin.Params {
	return gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(id), 10)}}
}

// seedGame 插入一条测试游戏
func seedGame(t *testing.T, name string) *models.Game {
	t.Helper()
	g := &models.Game{Name: name, Tag: "default", GameType: "H5"}
	if err := database.DB.Create(g).Error; err != nil {
		t.Fatalf("插入测试游戏失败: %v", err)
	}
	return g
}

// TestRecalcGameRating 校验综合评分聚合（平均值 + 人数）
func TestRecalcGameRating(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	g := seedGame(t, "聚合测试游戏")

	// 无评分 → 0 / 0
	avg, count, err := recalcGameRating(g.ID)
	if err != nil || avg != 0 || count != 0 {
		t.Fatalf("无评分时应为 0/0，实得 %v/%v err=%v", avg, count, err)
	}

	rows := []models.GameRating{
		{GameID: g.ID, RaterKey: "0000001", Score: 5},
		{GameID: g.ID, RaterKey: "0000002", Score: 4},
		{GameID: g.ID, RaterKey: "G01", Score: 3},
	}
	for i := range rows {
		if err := database.DB.Create(&rows[i]).Error; err != nil {
			t.Fatalf("插入评分失败: %v", err)
		}
	}

	avg, count, err = recalcGameRating(g.ID)
	if err != nil {
		t.Fatalf("重算失败: %v", err)
	}
	if count != 3 {
		t.Fatalf("评分人数应为 3，实得 %d", count)
	}
	if avg != 4 {
		t.Fatalf("平均分应为 4，实得 %v", avg)
	}

	// 缓存字段应已落库
	var stored models.Game
	if err := database.DB.First(&stored, g.ID).Error; err != nil {
		t.Fatalf("回读游戏失败: %v", err)
	}
	if stored.RatingCount != 3 || stored.AvgRating != 4 {
		t.Fatalf("缓存字段未更新：count=%d avg=%v", stored.RatingCount, stored.AvgRating)
	}
}

// TestRateGameUpsert 一人一票：重复评分只更新，不新增
func TestRateGameUpsert(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	g := seedGame(t, "评分提交游戏")

	rate := func(score int) (int, float64, int) {
		c, w := newCtx("POST", "/api/games/1/rating", map[string]any{"score": score},
			gameParam(g.ID), 7)
		c.Set("accountCode", "0000007")
		RateGame(c)
		if w.Code != 200 {
			t.Fatalf("提交评分失败，状态码 %d", w.Code)
		}
		avg, count, _ := recalcGameRating(g.ID)
		return score, avg, count
	}

	rate(5)
	_, avg, count := rate(2)
	if count != 1 {
		t.Fatalf("同一账号重复评分应只有 1 条，实得 %d", count)
	}
	if avg != 2 {
		t.Fatalf("改评后平均分应为 2，实得 %v", avg)
	}

	var rows []models.GameRating
	database.DB.Where("game_id = ?", g.ID).Find(&rows)
	if len(rows) != 1 || rows[0].Score != 2 {
		t.Fatalf("明细应为 1 条且分数 2，实得 %+v", rows)
	}
}

// TestRateGameValidation 非法分数应被拒绝
func TestRateGameValidation(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	g := seedGame(t, "评分校验游戏")
	c, w := newCtx("POST", "/api/games/1/rating", map[string]any{"score": 9},
		gameParam(g.ID), 8)
	c.Set("accountCode", "0000008")
	RateGame(c)
	if w.Code != 400 {
		t.Fatalf("非法分数应返回 400，实得 %d", w.Code)
	}
}

// TestUnrateGame 取消评分后聚合归零
func TestUnrateGame(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	g := seedGame(t, "取消评分游戏")
	if err := database.DB.Create(&models.GameRating{
		GameID: g.ID, RaterKey: "0000009", Score: 4,
	}).Error; err != nil {
		t.Fatalf("插入评分失败: %v", err)
	}
	if _, _, err := recalcGameRating(g.ID); err != nil {
		t.Fatalf("重算失败: %v", err)
	}

	c, w := newCtx("DELETE", "/api/games/1/rating", nil, gameParam(g.ID), 9)
	c.Set("accountCode", "0000009")
	UnrateGame(c)
	if w.Code != 200 {
		t.Fatalf("取消评分失败，状态码 %d", w.Code)
	}
	avg, count, _ := recalcGameRating(g.ID)
	if avg != 0 || count != 0 {
		t.Fatalf("取消后应归零，实得 %v/%v", avg, count)
	}
}

