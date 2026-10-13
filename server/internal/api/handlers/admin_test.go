package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// itoa uint → 十进制字符串
func itoa(id uint) string { return strconv.FormatUint(uint64(id), 10) }

// TestListUsers 列出用户（玩家表，裸数组）
func TestListUsers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	seedUser(t, "u1", "pw", "user")
	seedUser(t, "u2", "pw", "user")

	c, w := newCtx(http.MethodGet, "/api/admin/users", nil, nil, 1)
	ListUsers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var users []models.User
	_ = json.Unmarshal(w.Body.Bytes(), &users)
	if len(users) != 2 {
		t.Fatalf("期望 2 个玩家，实际 %d", len(users))
	}
}

// TestUpdateManager 管理员更新管理员账号
func TestUpdateManager(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	mgr := &models.SysManager{
		ManagerCode: "G01", Username: "helper", Password: "x",
		Nickname: "小管", Role: models.RoleAdmin, Permissions: "[]",
		IsEnabled: 1, CreatedAt: nowUnix(),
	}
	database.DB.Create(mgr)

	c, w := newCtx(http.MethodPut, "/api/admin/managers/"+itoa(mgr.ID),
		map[string]interface{}{"nickname": "改名", "is_enabled": 0, "allow_rank": 1},
		gin.Params{{Key: "id", Value: itoa(mgr.ID)}}, 1)
	UpdateManager(c)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var got models.SysManager
	database.DB.First(&got, mgr.ID)
	if got.Nickname != "改名" || got.IsEnabled != 0 || got.AllowRank != 1 {
		t.Fatalf("更新未生效: %+v", got)
	}
}

// TestDeleteManager_CannotDeleteSuper 禁止删除内置超管
func TestDeleteManager_CannotDeleteSuper(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	super := &models.SysManager{
		ManagerCode: database.SuperManagerCode, Username: "admin", Password: "x",
		Role: models.RoleSuperAdmin, Permissions: "[]", IsEnabled: 1, CreatedAt: nowUnix(),
	}
	database.DB.Create(super)

	c, w := newCtx(http.MethodDelete, "/api/admin/managers/"+itoa(super.ID), nil,
		gin.Params{{Key: "id", Value: itoa(super.ID)}}, 1)
	DeleteManager(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400（禁止删除），实际 %d", w.Code)
	}
}

// TestDeleteManager_Success 删除普通管理员（并解绑）
func TestDeleteManager_Success(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	mgr := &models.SysManager{
		ManagerCode: "G02", Username: "del", Password: "x",
		Role: models.RoleAdmin, Permissions: "[]", IsEnabled: 1, CreatedAt: nowUnix(),
	}
	database.DB.Create(mgr)
	bind := models.UserManagerBind{PlayerUID: "0000001", ManagerCode: "G02", CreatedAt: nowUnix()}
	database.DB.Create(&bind)

	c, w := newCtx(http.MethodDelete, "/api/admin/managers/"+itoa(mgr.ID), nil,
		gin.Params{{Key: "id", Value: itoa(mgr.ID)}}, 1)
	DeleteManager(c)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var count int64
	database.DB.Model(&models.SysManager{}).Where("id = ?", mgr.ID).Count(&count)
	if count != 0 {
		t.Fatal("删除未生效")
	}
	var bindCnt int64
	database.DB.Model(&models.UserManagerBind{}).Where("manager_code = ?", "G02").Count(&bindCnt)
	if bindCnt != 0 {
		t.Fatal("绑定未清理")
	}
}

// TestGetAndUpdateSettings 设置读取与更新
func TestGetAndUpdateSettings(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	c, w := newCtx(http.MethodPut, "/api/admin/settings",
		models.Setting{Key: "site_name", Value: "我的游戏站"}, nil, 1)
	UpdateSetting(c)
	if w.Code != http.StatusOK {
		t.Fatalf("更新期望 200，实际 %d", w.Code)
	}

	c2, w2 := newCtx(http.MethodGet, "/api/admin/settings", nil, nil, 1)
	GetSettings(c2)
	var resp struct {
		Settings []models.Setting `json:"settings"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if len(resp.Settings) != 1 || resp.Settings[0].Value != "我的游戏站" {
		t.Fatalf("设置错误: %+v", resp.Settings)
	}
}

// TestUpdateSetting_InvalidKey 空 key
func TestUpdateSetting_InvalidKey(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	c, w := newCtx(http.MethodPut, "/api/admin/settings", models.Setting{Value: "x"}, nil, 1)
	UpdateSetting(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d", w.Code)
	}
}

// TestAccessiblePaths 授权路径解析与访问控制
func TestAccessiblePaths(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	dir := t.TempDir()
	_ = os.Setenv("TRIM_DATA_SHARE_PATHS", dir)

	if !isPathAccessible(filepath.Join(dir, "sub")) {
		t.Fatal("授权目录内路径应可访问")
	}
	if isPathAccessible("/etc/passwd") {
		t.Fatal("越权路径应被拒绝")
	}
	if isPathAccessible("") {
		t.Fatal("空路径应被拒绝")
	}
	paths := accessiblePaths()
	if len(paths) != 1 || paths[0] != dir {
		t.Fatalf("授权路径解析错误: %+v", paths)
	}
}

// TestGetAccessiblePaths 接口返回
func TestGetAccessiblePaths(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	dir := t.TempDir()
	_ = os.Setenv("TRIM_DATA_SHARE_PATHS", dir)

	c, w := newCtx(http.MethodGet, "/api/admin/accessible-paths", nil, nil, 1)
	GetAccessiblePaths(c)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
}

// TestBrowseAccessiblePath 浏览授权目录
func TestBrowseAccessiblePath(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	_ = os.Setenv("TRIM_DATA_SHARE_PATHS", dir)

	c, w := newCtx(http.MethodGet, "/api/admin/accessible-paths/browse", nil, nil, 1)
	c.Request.URL.RawQuery = "path=" + strings.ReplaceAll(dir, `\`, "%5C")
	BrowseAccessiblePath(c)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
}

// TestBrowseAccessiblePath_Forbidden 越权浏览
func TestBrowseAccessiblePath_Forbidden(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	_ = os.Setenv("TRIM_DATA_SHARE_PATHS", t.TempDir())

	c, w := newCtx(http.MethodGet, "/api/admin/accessible-paths/browse", nil, nil, 1)
	c.Request.URL.RawQuery = "path=%2Fetc"
	BrowseAccessiblePath(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("期望 403，实际 %d", w.Code)
	}
}
