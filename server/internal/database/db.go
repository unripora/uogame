package database

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uogame-go/internal/config"
	"uogame-go/internal/models"
)

// SuperManagerCode 超级管理员的固定编码
const SuperManagerCode = "G00"

// DB 全局数据库句柄
var DB *gorm.DB

// nowUnix 当前 Unix 时间戳
func nowUnix() int64 { return time.Now().Unix() }

// Init 初始化 SQLite 连接并执行 AutoMigrate，随后确保默认管理员存在
func Init(cfg *config.Config) {
	// 确保数据库所在目录存在
	if dir := filepath.Dir(cfg.DBPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("创建数据库目录失败: %v", err)
		}
	}

	db, err := gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}

	// AutoMigrate 建表：表名/字段与原始结构一致，可兼容旧库
	if err := db.AutoMigrate(
		&models.User{},
		&models.SysManager{},
		&models.UserManagerBind{},
		&models.Game{},
		&models.Category{},
		&models.Setting{},
		&models.ChatMessage{},
		&models.UserPreference{},
		&models.UserSaveBackup{},
		&models.GameCategory{},
		&models.GameTag{},
		&models.GameRating{},
	); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	DB = db
	migrateGameMultiTags()
	// 双账号体系迁移：把 users 中的管理员迁出到 sys_manager，并补齐玩家 UID
	migrateDualAccountSystem()
	initSuperManager(cfg)
	migratePlayerUIDs()
}

// migrateGameMultiTags 将旧单值字段回填到多值关联表（幂等）
//   - 旧 category_id > 0 且关联表无记录 → 插入一条分类关联
//   - 旧 tag 非空且非 default 且关联表无记录 → 插入一条标签关联
func migrateGameMultiTags() {
	var games []models.Game
	if err := DB.Find(&games).Error; err != nil {
		log.Printf("[Migrate] 读取游戏失败: %v", err)
		return
	}
	for _, g := range games {
		if g.CategoryID > 0 {
			var cnt int64
			DB.Model(&models.GameCategory{}).Where("game_id = ?", g.ID).Count(&cnt)
			if cnt == 0 {
				DB.Create(&models.GameCategory{GameID: g.ID, CategoryID: g.CategoryID})
			}
		}
		if tag := g.Tag; tag != "" && tag != "default" {
			var cnt int64
			DB.Model(&models.GameTag{}).Where("game_id = ?", g.ID).Count(&cnt)
			if cnt == 0 {
				DB.Create(&models.GameTag{GameID: g.ID, Tag: tag})
			}
		}
	}
}

// migrateDualAccountSystem 双账号体系迁移（幂等）：
// 把 users 表中 role 为 admin/super_admin 的行迁移到 sys_manager 表（分配 G 编码），
// 并从 users 表删除，使 users 成为纯玩家表。管理员的存档/聊天/偏好数据保留在玩家侧。
func migrateDualAccountSystem() {
	var managers []models.User
	if err := DB.Where("role = ? OR role = ?", models.RoleAdmin, models.RoleSuperAdmin).Order("id asc").Find(&managers).Error; err != nil {
		log.Printf("[Migrate] 读取旧管理员失败: %v", err)
		return
	}
	if len(managers) == 0 {
		return
	}
	// 下一个管理员编码序号：已有 Gxx 数量
	var existCnt int64
	DB.Model(&models.SysManager{}).Count(&existCnt)
	nextIdx := int(existCnt) // G00 已被超管占用则由 initSuperManager 处理；此处从当前数量起算

	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, u := range managers {
			// 幂等：若该用户名已在 sys_manager 存在则跳过
			var dup int64
			tx.Model(&models.SysManager{}).Where("username = ?", u.Username).Count(&dup)
			if dup > 0 {
				if err := tx.Where("id = ?", u.ID).Delete(&models.User{}).Error; err != nil {
					return err
				}
				continue
			}
			code := formatManagerCode(nextIdx)
			m := models.SysManager{
				ManagerCode:         code,
				Username:            u.Username,
				Password:            u.Password,
				Nickname:            u.Nickname,
				Avatar:              u.Avatar,
				Role:                u.Role,
				Permissions:         u.Permissions,
				CategoryPermissions: u.CategoryPermissions,
				IsEnabled:           1,
				AllowRank:           0,
				CreatedAt:           nowUnix(),
			}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", u.ID).Delete(&models.User{}).Error; err != nil {
				return err
			}
			nextIdx++
			log.Printf("[Migrate] 管理员 %s 迁移至 sys_manager（%s）", u.Username, code)
		}
		return nil
	})
	if err != nil {
		log.Printf("[Migrate] 双账号体系迁移失败: %v", err)
	}
}

// initSuperManager 确保超级管理员存在（sys_manager，编码固定 G00）
func initSuperManager(cfg *config.Config) {
	var super models.SysManager
	err := DB.Where("manager_code = ?", SuperManagerCode).First(&super).Error
	if err == gorm.ErrRecordNotFound {
		hash, herr := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if herr != nil {
			log.Printf("生成管理员密码哈希失败: %v", herr)
			return
		}
		super = models.SysManager{
			ManagerCode:         SuperManagerCode,
			Username:            cfg.AdminUsername,
			Password:            string(hash),
			Nickname:            "超级管理员",
			Role:                models.RoleSuperAdmin,
			Permissions:         marshalAllPermissions(),
			CategoryPermissions: "[]",
			IsEnabled:           1,
			AllowRank:           0,
			CreatedAt:           nowUnix(),
		}
		if cerr := DB.Create(&super).Error; cerr != nil {
			log.Printf("创建超级管理员失败: %v", cerr)
		} else {
			log.Printf("[Admin] 已创建超级管理员: %s (%s)", cfg.AdminUsername, SuperManagerCode)
		}
		return
	}

	// 存在 .apply_credentials 标记时强制刷新超管密码
	flag := filepath.Join(cfg.DataDir, ".apply_credentials")
	if _, serr := os.Stat(flag); serr == nil {
		log.Printf("[Admin] Found .apply_credentials flag, forcing admin update.")
		hash, herr := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if herr == nil {
			super.Password = string(hash)
			super.Role = models.RoleSuperAdmin
			super.Permissions = marshalAllPermissions()
			super.IsEnabled = 1
			if uerr := DB.Save(&super).Error; uerr != nil {
				log.Printf("更新管理员凭据失败: %v", uerr)
			}
		}
		_ = os.Remove(flag)
	}
}

// migratePlayerUIDs 为没有 UID 的玩家补齐 7 位编码（0000001 起，按 id 顺序，幂等）
func migratePlayerUIDs() {
	var players []models.User
	if err := DB.Where("uid IS NULL OR uid = ''").Order("id asc").Find(&players).Error; err != nil {
		log.Printf("[Migrate] 读取待补 UID 玩家失败: %v", err)
		return
	}
	if len(players) == 0 {
		return
	}
	// 已存在的最大 UID 序号
	var maxUID string
	DB.Model(&models.User{}).Where("uid <> ''").Order("uid desc").Limit(1).Pluck("uid", &maxUID)
	next := parseUIDSeq(maxUID) + 1
	for _, p := range players {
		uid := formatPlayerUID(next)
		if err := DB.Model(&models.User{}).Where("id = ?", p.ID).Update("uid", uid).Error; err != nil {
			log.Printf("[Migrate] 玩家 %d 补 UID 失败: %v", p.ID, err)
			continue
		}
		next++
	}
	log.Printf("[Migrate] 已为 %d 名玩家补齐 UID", len(players))
}

// formatPlayerUID 生成 7 位玩家编码
func formatPlayerUID(seq int) string {
	return fmt.Sprintf("%07d", seq)
}

// formatManagerCode 生成管理员编码：0 → G00，1 → G01 ...
func formatManagerCode(idx int) string {
	return fmt.Sprintf("G%02d", idx)
}

// parseUIDSeq 从 7 位字符串解析序号（容错）
func parseUIDSeq(uid string) int {
	if uid == "" {
		return 0
	}
	n := 0
	for _, ch := range uid {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// marshalAllPermissions 序列化全部权限点为 JSON 数组
func marshalAllPermissions() string {
	b, err := json.Marshal(models.AllPermissions)
	if err != nil {
		return "[]"
	}
	return string(b)
}
