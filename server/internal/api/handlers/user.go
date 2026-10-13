package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"uogame-go/internal/api/middleware"
	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// GetUserProfile 获取当前账号资料（玩家或管理员）
func GetUserProfile(c *gin.Context) {
	userID, _ := c.Get("userID")
	if middleware.IsManagerRequest(c) {
		var mgr models.SysManager
		if err := database.DB.First(&mgr, userID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user": mgr})
		return
	}
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// UpdateUserProfile 更新当前账号资料（用户名、昵称、头像、密码）
// 玩家与管理员分别更新各自表；用户名变更后重新签发 token
func UpdateUserProfile(c *gin.Context) {
	cfg := GetConfig(c)
	userID, _ := c.Get("userID")

	var payload struct {
		Username string `json:"username"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if middleware.IsManagerRequest(c) {
		var mgr models.SysManager
		if err := database.DB.First(&mgr, userID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		newUsername := ""
		if payload.Username != "" && payload.Username != mgr.Username {
			var count int64
			database.DB.Model(&models.SysManager{}).Where("username = ?", payload.Username).Count(&count)
			if count > 0 {
				c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
				return
			}
			mgr.Username = payload.Username
			newUsername = payload.Username
		}
		if payload.Nickname != "" {
			mgr.Nickname = payload.Nickname
		}
		if payload.Avatar != "" {
			mgr.Avatar = payload.Avatar
		}
		if payload.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
				return
			}
			mgr.Password = string(hash)
		}
		if err := database.DB.Save(&mgr).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
			return
		}
		if newUsername != "" {
			if token, err := middleware.GenerateToken(mgr.ID, mgr.Username, mgr.Role, models.AccountTypeManager, mgr.ManagerCode, cfg.JWTSecret); err == nil {
				middleware.SetAuthCookie(c, cfg, token)
				c.JSON(http.StatusOK, gin.H{"message": "更新成功", "user": mgr, "token": token})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"message": "更新成功", "user": mgr})
		return
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	// 用户名变更：校验非空、唯一
	newUsername := ""
	if payload.Username != "" && payload.Username != user.Username {
		var count int64
		database.DB.Model(&models.User{}).Where("username = ?", payload.Username).Count(&count)
		if count > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
			return
		}
		user.Username = payload.Username
		newUsername = payload.Username
	}
	if payload.Nickname != "" {
		user.Nickname = payload.Nickname
	}
	if payload.Avatar != "" {
		user.Avatar = payload.Avatar
	}
	if payload.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
			return
		}
		user.Password = string(hash)
	}
	if err := database.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	// 用户名变更后重新签发 token（Cookie + 响应体同步更新）
	if newUsername != "" {
		if token, err := middleware.GenerateToken(user.ID, user.Username, user.Role, models.AccountTypePlayer, user.UID, cfg.JWTSecret); err == nil {
			middleware.SetAuthCookie(c, cfg, token)
			c.JSON(http.StatusOK, gin.H{"message": "更新成功", "user": user, "token": token})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "更新成功", "user": user})
}

// GetUserSave 获取用户云存档（用户偏好 cloud_save_localstorage）
func GetUserSave(c *gin.Context) {
	userID, _ := c.Get("userID")
	var pref models.UserPreference
	if err := database.DB.Where("user_id = ? AND key = ?", userID, "cloud_save_localstorage").First(&pref).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"data": ""})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pref.Value})
}

// UpdateUserSave 更新用户云存档
func UpdateUserSave(c *gin.Context) {
	userID, _ := c.Get("userID")
	var payload struct {
		Data string `json:"data"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	pref := models.UserPreference{
		UserID:    toUint(userID),
		Key:       "cloud_save_localstorage",
		Value:     payload.Data,
		UpdatedAt: time.Now().Unix(),
	}
	// 复合主键 upsert
	if err := database.DB.Save(&pref).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "存档已同步"})
}

// RecordPlayTime 记录游玩时长（累加到玩家 total_play_time 与游戏 hot_time）
//   - 玩家身份：直接累加到自身
//   - 管理员身份：记到其绑定的玩家账号；若未开启 AllowRank（默认否）则不计入榜单，
//     但游戏热度照常累加（管理员游玩仍会提升游戏热度）
func RecordPlayTime(c *gin.Context) {
	userID, _ := c.Get("userID")
	var payload struct {
		GameID   uint    `json:"game_id"`
		Duration float64 `json:"duration"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if middleware.IsManagerRequest(c) {
		var mgr models.SysManager
		if err := database.DB.First(&mgr, userID).Error; err == nil && mgr.AllowRank == 1 {
			// 记到绑定的玩家账号
			var bind models.UserManagerBind
			if err := database.DB.Where("manager_code = ?", mgr.ManagerCode).First(&bind).Error; err == nil {
				var player models.User
				if err := database.DB.Where("uid = ?", bind.PlayerUID).First(&player).Error; err == nil {
					player.TotalPlayTime += payload.Duration
					database.DB.Save(&player)
				}
			}
		}
		// 游戏热度照常累加
		accumulateGameHotTime(payload.GameID, payload.Duration)
		c.JSON(http.StatusOK, gin.H{"message": "已记录"})
		return
	}

	// 玩家身份：累加自身总时长
	var user models.User
	if err := database.DB.First(&user, userID).Error; err == nil {
		user.TotalPlayTime += payload.Duration
		database.DB.Save(&user)
	}
	accumulateGameHotTime(payload.GameID, payload.Duration)
	c.JSON(http.StatusOK, gin.H{"message": "已记录"})
}

// accumulateGameHotTime 累加指定游戏的热度时长
func accumulateGameHotTime(gameID uint, duration float64) {
	if gameID == 0 {
		return
	}
	var game models.Game
	if err := database.DB.First(&game, gameID).Error; err == nil {
		game.HotTime += duration
		database.DB.Save(&game)
	}
}

// BackupListItem 存档备份列表项（不含大字段 data，仅返回体积）
type BackupListItem struct {
	ID        uint   `json:"id"`
	UserID    uint   `json:"user_id"`
	Title     string `json:"title"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"created_at"`
}

// ListSaveBackups 列出当前用户的存档备份
func ListSaveBackups(c *gin.Context) {
	userID, _ := c.Get("userID")
	var backups []models.UserSaveBackup
	if err := database.DB.Where("user_id = ?", userID).Order("created_at desc").Find(&backups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch backups"})
		return
	}
	items := make([]BackupListItem, 0, len(backups))
	for _, b := range backups {
		items = append(items, BackupListItem{
			ID:        b.ID,
			UserID:    b.UserID,
			Title:     b.Title,
			Size:      int64(len(b.Data)),
			CreatedAt: b.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"backups": items})
}

// CreateSaveBackup 新建存档备份
func CreateSaveBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	var payload struct {
		Title string `json:"title"`
		Data  string `json:"data"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	backup := models.UserSaveBackup{
		UserID:    toUint(userID),
		Title:     payload.Title,
		Data:      payload.Data,
		CreatedAt: time.Now().Unix(),
	}
	if err := database.DB.Create(&backup).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "备份成功", "id": backup.ID})
}

// UpdateSaveBackupTitle 重命名存档备份
func UpdateSaveBackupTitle(c *gin.Context) {
	userID, _ := c.Get("userID")
	id, _ := strconv.Atoi(c.Param("id"))
	var backup models.UserSaveBackup
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&backup).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Backup not found"})
		return
	}
	var payload struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	backup.Title = payload.Title
	if err := database.DB.Save(&backup).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update backup"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "重命名成功"})
}

// DeleteSaveBackup 删除存档备份
func DeleteSaveBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	id, _ := strconv.Atoi(c.Param("id"))
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).Delete(&models.UserSaveBackup{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete backup"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// RestoreSaveBackup 恢复存档备份（将备份内容回写到本地存档）
func RestoreSaveBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	id, _ := strconv.Atoi(c.Param("id"))
	var backup models.UserSaveBackup
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&backup).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Backup not found"})
		return
	}
	pref := models.UserPreference{
		UserID:    toUint(userID),
		Key:       "cloud_save_localstorage",
		Value:     backup.Data,
		UpdatedAt: time.Now().Unix(),
	}
	if err := database.DB.Save(&pref).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "恢复成功", "data": backup.Data})
}

// ExportSaveBackup 导出存档备份为 JSON 附件
func ExportSaveBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	id, _ := strconv.Atoi(c.Param("id"))
	var backup models.UserSaveBackup
	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).First(&backup).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Backup not found"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\"backup.json\"")
	c.JSON(http.StatusOK, gin.H{
		"title":      backup.Title,
		"data":       backup.Data,
		"created_at": backup.CreatedAt,
	})
}

// ImportSaveBackup 从上传的 JSON 导入存档备份
func ImportSaveBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	var payload struct {
		Title string `json:"title"`
		Data  string `json:"data"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid backup data content"})
		return
	}
	if payload.Data == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid backup data content"})
		return
	}
	// 校验 data 是否为合法 JSON（对齐原版 Invalid backup file format）
	if !json.Valid([]byte(payload.Data)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid backup file format"})
		return
	}
	backup := models.UserSaveBackup{
		UserID:    toUint(userID),
		Title:     payload.Title,
		Data:      payload.Data,
		CreatedAt: time.Now().Unix(),
	}
	if err := database.DB.Create(&backup).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save to database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "导入成功", "id": backup.ID})
}

// toUint 将上下文中的 userID（interface{}）安全转换为 uint
func toUint(v interface{}) uint {
	switch n := v.(type) {
	case uint:
		return n
	case int:
		return uint(n)
	case int64:
		return uint(n)
	case float64:
		return uint(n)
	default:
		return 0
	}
}
