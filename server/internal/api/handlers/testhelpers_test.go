package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uogame-go/internal/config"
	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// setupTestDB 用临时内存库替换全局 DB，并注入测试配置；返回清理函数
func setupTestDB(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevDB := database.DB
	prevCfg := Cfg

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.SysManager{}, &models.UserManagerBind{},
		&models.Setting{}, &models.ChatMessage{},
		&models.Game{}, &models.GameRating{},
	); err != nil {
		t.Fatalf("测试库迁移失败: %v", err)
	}
	database.DB = db
	Cfg = &config.Config{JWTSecret: "test-secret", AuthCookie: "auth_token"}

	return func() {
		database.DB = prevDB
		Cfg = prevCfg
	}
}

// seedUser 在玩家表插入一条测试用户（密码经 bcrypt 哈希）
func seedUser(t *testing.T, username, password, role string) *models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	u := &models.User{
		Username:            username,
		Password:            string(hash),
		Nickname:            username,
		Role:                role,
		Permissions:         "[]",
		CategoryPermissions: "[]",
	}
	if err := database.DB.Create(u).Error; err != nil {
		t.Fatalf("插入测试用户失败: %v", err)
	}
	return u
}

// newCtx 构造带 JSON body 与上下文变量的 gin.Context 及响应记录器
func newCtx(method, path string, body interface{}, params gin.Params, userID uint) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = params

	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	if userID != 0 {
		c.Set("userID", userID)
	}
	return c, w
}
