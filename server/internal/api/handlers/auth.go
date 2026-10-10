package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"uogame-go/internal/api/middleware"
	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// loginRequest 登录请求体
// account_type 可选：player / manager；不传时自动识别（同名仅存于一表）或要求前端选择
type loginRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	AccountType string `json:"account_type"`
}

// registerRequest 注册请求体
type registerRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Nickname string `json:"nickname"`
}

// identityInfo 登录/校验返回的身份信息（供前端展示与切换）
type identityInfo struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	Role        string `json:"role"`
	AccountType string `json:"account_type"`
	AccountCode string `json:"account_code"`
}

// Login 登录：支持玩家/管理员双体系
//   - 同名账号可能同时存在于玩家表与管理员表，此时需前端传 account_type 明确
//   - 成功返回 user 对象（含 account_type/account_code）与 token
func Login(c *gin.Context) {
	cfg := GetConfig(c)
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	var player models.User
	playerErr := database.DB.Where("username = ?", req.Username).First(&player).Error
	var mgr models.SysManager
	mgrErr := database.DB.Where("username = ?", req.Username).First(&mgr).Error

	// 判定账号体系
	accountType := req.AccountType
	if accountType == "" {
		switch {
		case playerErr == nil && mgrErr == nil:
			// 两表都存在 → 要求前端指定身份
			c.JSON(http.StatusConflict, gin.H{
				"error":             "该用户名既是玩家又是管理员，请选择登录身份",
				"require_identity":  true,
				"available_types":   []string{models.AccountTypePlayer, models.AccountTypeManager},
			})
			return
		case mgrErr == nil:
			accountType = models.AccountTypeManager
		default:
			accountType = models.AccountTypePlayer
		}
	}

	if accountType == models.AccountTypeManager {
		if mgrErr != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(mgr.Password), []byte(req.Password)) != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
			return
		}
		if mgr.IsEnabled == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "该管理员账号已被禁用"})
			return
		}
		token, err := middleware.GenerateToken(mgr.ID, mgr.Username, mgr.Role, models.AccountTypeManager, mgr.ManagerCode, cfg.JWTSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
			return
		}
		middleware.SetAuthCookie(c, cfg, token)
		c.JSON(http.StatusOK, gin.H{"message": "登录成功", "user": mgr, "token": token})
		return
	}

	// 玩家登录
	if playerErr != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(player.Password), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	token, err := middleware.GenerateToken(player.ID, player.Username, player.Role, models.AccountTypePlayer, player.UID, cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}
	middleware.SetAuthCookie(c, cfg, token)
	c.JSON(http.StatusOK, gin.H{"message": "登录成功", "user": player, "token": token})
}

// SwitchIdentity 免密一键切换身份（基于 user_manager_bind 绑定关系）
// 请求体：{target_type: player|manager}
func SwitchIdentity(c *gin.Context) {
	cfg := GetConfig(c)
	curType := middleware.CurrentAccountType(c)
	curID, _ := c.Get("userID")
	id, _ := curID.(uint)

	var req struct {
		TargetType string `json:"target_type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TargetType == curType {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的切换目标"})
		return
	}

	if curType == models.AccountTypeManager {
		// 管理员 → 其绑定的玩家
		var mgr models.SysManager
		if err := database.DB.First(&mgr, id).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		var binds []models.UserManagerBind
		database.DB.Where("manager_code = ?", mgr.ManagerCode).Find(&binds)
		if len(binds) == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "未绑定玩家账号"})
			return
		}
		var player models.User
		if err := database.DB.Where("uid = ?", binds[0].PlayerUID).First(&player).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "绑定的玩家账号不存在"})
			return
		}
		token, err := middleware.GenerateToken(player.ID, player.Username, player.Role, models.AccountTypePlayer, player.UID, cfg.JWTSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
			return
		}
		middleware.SetAuthCookie(c, cfg, token)
		c.JSON(http.StatusOK, gin.H{"message": "已切换为玩家", "user": player, "token": token})
		return
	}

	// 玩家 → 其绑定的管理员
	var player models.User
	if err := database.DB.First(&player, id).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	var binds []models.UserManagerBind
	database.DB.Where("player_uid = ?", player.UID).Find(&binds)
	if len(binds) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "未绑定管理员账号"})
		return
	}
	var mgr models.SysManager
	if err := database.DB.Where("manager_code = ?", binds[0].ManagerCode).First(&mgr).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "绑定的管理员账号不存在"})
		return
	}
	if mgr.IsEnabled == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "管理员账号已被禁用"})
		return
	}
	token, err := middleware.GenerateToken(mgr.ID, mgr.Username, mgr.Role, models.AccountTypeManager, mgr.ManagerCode, cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}
	middleware.SetAuthCookie(c, cfg, token)
	c.JSON(http.StatusOK, gin.H{"message": "已切换为管理员", "user": mgr, "token": token})
}

// Register 玩家注册（受系统注册开关控制），自动分配 7 位 UID
func Register(c *gin.Context) {
	cfg := GetConfig(c)
	if !isRegistrationOpen() {
		c.JSON(http.StatusForbidden, gin.H{"error": "注册功能已关闭"})
		return
	}

	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	var count int64
	database.DB.Model(&models.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	nickname := req.Nickname
	if nickname == "" {
		nickname = req.Username
	}
	user := models.User{
		UID:                 nextPlayerUID(),
		Username:            req.Username,
		Password:            string(hash),
		Nickname:            nickname,
		Role:                models.RoleUser,
		CategoryPermissions: "[]",
		Permissions:         "[]",
	}
	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}

	token, err := middleware.GenerateToken(user.ID, user.Username, user.Role, models.AccountTypePlayer, user.UID, cfg.JWTSecret)
	if err == nil {
		middleware.SetAuthCookie(c, cfg, token)
	}
	c.JSON(http.StatusOK, gin.H{"message": "注册成功", "user": user, "token": token})
}

// Logout 登出，清除 Cookie
func Logout(c *gin.Context) {
	cfg := GetConfig(c)
	middleware.ClearAuthCookie(c, cfg)
	c.JSON(http.StatusOK, gin.H{"message": "已退出登录"})
}

// Verify 校验当前登录态并返回用户信息（玩家或管理员）
// 前端约定：响应必须包含 authenticated 布尔字段，否则前端会清除本地登录态
func Verify(c *gin.Context) {
	userID, _ := c.Get("userID")
	id, _ := userID.(uint)
	accountType := middleware.CurrentAccountType(c)

	// 附带可切换身份信息，供前端"身份切换"入口
	var sw *identityInfo

	if accountType == models.AccountTypeManager {
		var mgr models.SysManager
		if err := database.DB.First(&mgr, id).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		if mgr.IsEnabled == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		sw = findBoundPlayerIdentity(mgr.ManagerCode)
		c.JSON(http.StatusOK, gin.H{"authenticated": true, "user": mgr, "switch_identity": sw})
		return
	}

	var user models.User
	if err := database.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	sw = findBoundManagerIdentity(user.UID)
	c.JSON(http.StatusOK, gin.H{"authenticated": true, "user": user, "switch_identity": sw})
}

// findBoundManagerIdentity 查询玩家绑定的管理员身份
func findBoundManagerIdentity(playerUID string) *identityInfo {
	if playerUID == "" {
		return nil
	}
	var bind models.UserManagerBind
	if err := database.DB.Where("player_uid = ?", playerUID).First(&bind).Error; err != nil {
		return nil
	}
	var mgr models.SysManager
	if err := database.DB.Where("manager_code = ?", bind.ManagerCode).First(&mgr).Error; err != nil {
		return nil
	}
	if mgr.IsEnabled == 0 {
		return nil
	}
	return &identityInfo{ID: mgr.ID, Username: mgr.Username, Nickname: mgr.Nickname, Role: mgr.Role, AccountType: models.AccountTypeManager, AccountCode: mgr.ManagerCode}
}

// findBoundPlayerIdentity 查询管理员绑定的玩家身份
func findBoundPlayerIdentity(managerCode string) *identityInfo {
	if managerCode == "" {
		return nil
	}
	var bind models.UserManagerBind
	if err := database.DB.Where("manager_code = ?", managerCode).First(&bind).Error; err != nil {
		return nil
	}
	var player models.User
	if err := database.DB.Where("uid = ?", bind.PlayerUID).First(&player).Error; err != nil {
		return nil
	}
	return &identityInfo{ID: player.ID, Username: player.Username, Nickname: player.Nickname, Role: player.Role, AccountType: models.AccountTypePlayer, AccountCode: player.UID}
}

// nextPlayerUID 生成下一个 7 位玩家 UID（0000001 起）
func nextPlayerUID() string {
	var maxUID string
	database.DB.Model(&models.User{}).Where("uid <> ''").Order("uid desc").Limit(1).Pluck("uid", &maxUID)
	seq := 0
	for _, ch := range maxUID {
		if ch < '0' || ch > '9' {
			seq = 0
			break
		}
		seq = seq*10 + int(ch-'0')
	}
	return fmt.Sprintf("%07d", seq+1)
}

// nowUnix 当前 Unix 时间戳
func nowUnix() int64 { return time.Now().Unix() }

// nextManagerCode 生成下一个管理员编码（G00 已由超管占用，故新管理员从 G01 起）
func nextManagerCode() string {
	var codes []string
	database.DB.Model(&models.SysManager{}).Where("manager_code LIKE 'G%'").Pluck("manager_code", &codes)
	maxIdx := -1
	for _, code := range codes {
		// 仅解析 "G" + 纯数字（跳过 G00 超管等由编号自然覆盖）
		if len(code) < 2 || code[0] != 'G' {
			continue
		}
		n, err := strconv.Atoi(code[1:])
		if err != nil {
			continue
		}
		if n > maxIdx {
			maxIdx = n
		}
	}
	return fmt.Sprintf("G%02d", maxIdx+1)
}

// isRegistrationOpen 读取注册开关设置（默认关闭：未配置即视为关闭）
func isRegistrationOpen() bool {
	var s models.Setting
	if err := database.DB.Where("key = ?", "registration_enabled").First(&s).Error; err != nil {
		return false
	}
	return s.Value == "true" || s.Value == "1"
}
