package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// ListUsers 列出全部玩家（纯玩家表，含 UID）
// 前端约定：返回**裸数组**（前端直接 .find(...)）
func ListUsers(c *gin.Context) {
	var users []models.User
	if err := database.DB.Order("id asc").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}
	if users == nil {
		users = []models.User{}
	}
	c.JSON(http.StatusOK, users)
}

// ListManagers 列出全部管理员（sys_manager）
func ListManagers(c *gin.Context) {
	var managers []models.SysManager
	if err := database.DB.Order("id asc").Find(&managers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch managers"})
		return
	}
	if managers == nil {
		managers = []models.SysManager{}
	}
	c.JSON(http.StatusOK, managers)
}

// CreateManager 创建管理员账号（自动分配 Gxx 编码）
func CreateManager(c *gin.Context) {
	var payload struct {
		Username    string   `json:"username" binding:"required"`
		Password    string   `json:"password" binding:"required"`
		Nickname    string   `json:"nickname"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	var count int64
	database.DB.Model(&models.SysManager{}).Where("username = ?", payload.Username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
		return
	}
	role := payload.Role
	if role != models.RoleAdmin {
		role = models.RoleAdmin // 不允许直接创建 super_admin
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	nickname := payload.Nickname
	if nickname == "" {
		nickname = payload.Username
	}
	perms := cleanPermissions(payload.Permissions)
	if perms == nil {
		perms = []string{}
	}
	mgr := models.SysManager{
		ManagerCode: nextManagerCode(),
		Username:    payload.Username,
		Password:    string(hash),
		Nickname:    nickname,
		Role:        role,
		Permissions: marshalPermissions(perms),
		IsEnabled:   1,
		AllowRank:   0,
		CreatedAt:   nowUnix(),
	}
	if err := database.DB.Create(&mgr).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "创建成功", "manager": mgr})
}

// PromotePlayer 将已有玩家「提权」为管理员，并在同一事务内自动建立人员绑定
// 前端约定：POST /api/admin/users/:id/promote { username, password, nickname, permissions }
// 提权后该玩家与新建管理员自动关联，登录后可在「我的」免密切换身份
func PromotePlayer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user id"})
		return
	}
	var payload struct {
		Username    string   `json:"username"`
		Password    string   `json:"password" binding:"required"`
		Nickname    string   `json:"nickname"`
		Permissions []string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写管理员密码"})
		return
	}
	var player models.User
	if err := database.DB.First(&player, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "玩家账号不存在"})
		return
	}
	// 玩家必须尚未绑定管理员
	var exist int64
	database.DB.Model(&models.UserManagerBind{}).Where("player_uid = ?", player.UID).Count(&exist)
	if exist > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该玩家已绑定管理员账号"})
		return
	}
	// 管理员用户名：默认沿用玩家用户名（便于同名双表 → 登录时选择身份）
	username := strings.TrimSpace(payload.Username)
	if username == "" {
		username = player.Username
	}
	var dup int64
	database.DB.Model(&models.SysManager{}).Where("username = ?", username).Count(&dup)
	if dup > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "管理员用户名已存在，请更换用户名"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	nickname := payload.Nickname
	if nickname == "" {
		nickname = player.Nickname
	}
	if nickname == "" {
		nickname = username
	}
	mgr := models.SysManager{
		ManagerCode: nextManagerCode(),
		Username:    username,
		Password:    string(hash),
		Nickname:    nickname,
		Role:        models.RoleAdmin, // 不允许直接提权为 super_admin
		Permissions: marshalPermissions(cleanPermissions(payload.Permissions)),
		IsEnabled:   1,
		AllowRank:   0,
		CreatedAt:   nowUnix(),
	}
	// 事务：创建管理员 + 自动绑定玩家，任一失败整体回滚
	var bind models.UserManagerBind
	createErr := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&mgr).Error; err != nil {
			return err
		}
		bind = models.UserManagerBind{
			PlayerUID:   player.UID,
			ManagerCode: mgr.ManagerCode,
			CreatedAt:   nowUnix(),
		}
		return tx.Create(&bind).Error
	})
	if createErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "提权成功", "manager": mgr, "bind": bind})
}

// UpdateManager 管理员更新管理员账号（角色/权限/启用/计榜/重置密码/昵称）
func UpdateManager(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var mgr models.SysManager
	if err := database.DB.First(&mgr, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Manager not found"})
		return
	}
	var payload struct {
		Nickname    string   `json:"nickname"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
		IsEnabled   *int     `json:"is_enabled"`
		AllowRank   *int     `json:"allow_rank"`
		Password    string   `json:"password"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if payload.Nickname != "" {
		mgr.Nickname = payload.Nickname
	}
	// 内置超管不可降级，且始终拥有全部权限
	if mgr.Role == models.RoleSuperAdmin || mgr.ManagerCode == database.SuperManagerCode {
		// 保持 super_admin 角色与全量权限
		mgr.Role = models.RoleSuperAdmin
		mgr.Permissions = marshalPermissions(models.AllPermissions)
		// 超管不可禁用
		if payload.IsEnabled != nil {
			mgr.IsEnabled = 1
		}
	} else {
		if payload.Role != "" {
			if !isValidManagerRole(payload.Role) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role"})
				return
			}
			mgr.Role = payload.Role
			if mgr.Role == models.RoleSuperAdmin {
				mgr.Permissions = marshalPermissions(models.AllPermissions)
			}
		}
		if payload.Permissions != nil {
			mgr.Permissions = marshalPermissions(cleanPermissions(payload.Permissions))
		}
		if payload.IsEnabled != nil {
			mgr.IsEnabled = *payload.IsEnabled
		}
	}
	if payload.AllowRank != nil {
		mgr.AllowRank = *payload.AllowRank
	}
	if payload.Password != "" {
		if hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost); err == nil {
			mgr.Password = string(hash)
		}
	}
	if err := database.DB.Save(&mgr).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update manager"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "更新成功", "manager": mgr})
}

// DeleteManager 删除管理员账号（禁止删除内置超管；同时解绑其人员关联）
func DeleteManager(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var mgr models.SysManager
	if err := database.DB.First(&mgr, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Manager not found"})
		return
	}
	if mgr.ManagerCode == database.SuperManagerCode || mgr.Role == models.RoleSuperAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete system admin"})
		return
	}
	database.DB.Where("manager_code = ?", mgr.ManagerCode).Delete(&models.UserManagerBind{})
	if err := database.DB.Delete(&models.SysManager{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete manager"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ListBinds 列出所有人员绑定
func ListBinds(c *gin.Context) {
	var binds []models.UserManagerBind
	if err := database.DB.Order("id asc").Find(&binds).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch binds"})
		return
	}
	if binds == nil {
		binds = []models.UserManagerBind{}
	}
	c.JSON(http.StatusOK, binds)
}

// CreateBind 新建人员绑定（玩家 UID + 管理员编码）
func CreateBind(c *gin.Context) {
	var payload struct {
		PlayerUID   string `json:"player_uid" binding:"required"`
		ManagerCode string `json:"manager_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	// 校验玩家与管理员的 uid
	var player models.User
	if err := database.DB.Where("uid = ?", payload.PlayerUID).First(&player).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "玩家账号不存在"})
		return
	}
	var mgr models.SysManager
	if err := database.DB.Where("manager_code = ?", payload.ManagerCode).First(&mgr).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "管理员账号不存在"})
		return
	}
	// 校验玩家是否已绑定
	var exist int64
	database.DB.Model(&models.UserManagerBind{}).Where("player_uid = ?", payload.PlayerUID).Count(&exist)
	if exist > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该玩家已绑定管理员"})
		return
	}
	bind := models.UserManagerBind{
		PlayerUID:   payload.PlayerUID,
		ManagerCode: payload.ManagerCode,
		CreatedAt:   nowUnix(),
	}
	if err := database.DB.Create(&bind).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "绑定成功", "bind": bind})
}

// DeleteBind 删除人员绑定（撤销关联，不影响玩家账号）
func DeleteBind(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := database.DB.Delete(&models.UserManagerBind{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete bind"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "解绑成功"})
}

// isValidManagerRole 校验管理员角色取值
func isValidManagerRole(role string) bool {
	return role == models.RoleAdmin || role == models.RoleSuperAdmin
}

// cleanPermissions 去重并过滤掉非法权限点
func cleanPermissions(in []string) []string {
	valid := make(map[string]bool, len(models.AllPermissions))
	for _, p := range models.AllPermissions {
		valid[p] = true
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		if valid[p] && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// marshalPermissions 序列化权限点为 JSON 数组字符串
func marshalPermissions(perms []string) string {
	if perms == nil {
		perms = []string{}
	}
	b, err := json.Marshal(perms)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// AdminClearChat 管理员清空聊天记录
func AdminClearChat(c *gin.Context) {
	if err := database.DB.Where("1 = 1").Delete(&models.ChatMessage{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear chat history"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "聊天记录已清空"})
}

// GetSettings 返回全部系统设置
func GetSettings(c *gin.Context) {
	var settings []models.Setting
	if err := database.DB.Find(&settings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

// UpdateSetting 更新单个设置项
func UpdateSetting(c *gin.Context) {
	var payload models.Setting
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if payload.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}
	// upsert
	if err := database.DB.Save(&payload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "更新成功", "setting": payload})
}

// GetAccessiblePaths 返回应用被授权的可访问路径列表
func GetAccessiblePaths(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"paths": accessiblePaths()})
}

// BrowseAccessiblePath 浏览授权目录下的文件（禁止越权访问）
func BrowseAccessiblePath(c *gin.Context) {
	reqPath := c.Query("path")
	if reqPath == "" {
		reqPath = c.Query("dir")
	}
	if !isPathAccessible(reqPath) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: path out of authorized range"})
		return
	}
	entries, err := os.ReadDir(reqPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Access denied"})
		return
	}
	type item struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}
	list := make([]item, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		var size int64
		if info != nil {
			size = info.Size()
		}
		list = append(list, item{
			Name:  e.Name(),
			Path:  filepath.Join(reqPath, e.Name()),
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	c.JSON(http.StatusOK, gin.H{"path": reqPath, "entries": list})
}

// accessiblePaths 从环境变量 TRIM_DATA_SHARE_PATHS 解析授权路径
func accessiblePaths() []string {
	raw := os.Getenv("TRIM_DATA_SHARE_PATHS")
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ":")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isPathAccessible 判断目标路径是否落在授权范围内
func isPathAccessible(target string) bool {
	if target == "" {
		return false
	}
	clean := filepath.Clean(target)
	for _, base := range accessiblePaths() {
		absBase := filepath.Clean(base)
		if clean == absBase || strings.HasPrefix(clean, absBase+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
