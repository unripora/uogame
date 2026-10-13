package models

import (
	"encoding/json"

	"github.com/golang-jwt/jwt/v5"
)

// 角色常量
const (
	RoleUser       = "user"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super_admin"
)

// 账号体系常量：玩家 / 管理员两套体系
const (
	AccountTypePlayer  = "player"  // 玩家（存档 / 游玩时长 / 榜单）
	AccountTypeManager = "manager" // 管理员（后台管理）
)

// 权限点常量（RBAC）：admin 角色按勾选的权限点放行，super_admin 拥有全部
const (
	PermGameManage        = "game.manage"        // 游戏管理：增删改/导入/批量导出/删除
	PermCategoryManage    = "category.manage"    // 分类管理：分类增删改、白名单配置
	PermUserManage        = "user.manage"        // 用户管理：列表/改角色/删除
	PermSettingManage     = "setting.manage"     // 系统设置：设置读写、可访问路径
	PermChatManage        = "chat.manage"        // 聊天管理：清空聊天记录
	PermLeaderboardManage = "leaderboard.manage" // 榜单管理：重置排行榜
)

// SuperAdminID 超级管理员的固定 ID（唯一，普通用户从 2 开始自增）
// 展示层将超管格式化为 0000000、第 n 个用户格式化为 000000n（仅前端展示，不改主键）
const SuperAdminID uint = 1

// AllPermissions 全部权限点（用于 super_admin 与默认全选）
var AllPermissions = []string{
	PermGameManage,
	PermCategoryManage,
	PermUserManage,
	PermSettingManage,
	PermChatManage,
	PermLeaderboardManage,
}

// User 玩家模型（原 users 表语义化为玩家表）
// UID：对外展示的 7 位玩家编码（0000001 起），与自增 ID 解耦
type User struct {
	ID                  uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	UID                 string  `gorm:"type:text;index" json:"uid"` // 玩家编码 0000001 起
	Username            string  `gorm:"unique;not null" json:"username"`
	Password            string  `gorm:"not null" json:"-"`
	Nickname            string  `gorm:"type:text" json:"nickname"`
	Avatar              string  `gorm:"type:text" json:"avatar"`
	Role                string  `gorm:"type:text" json:"role"` // 玩家固定 user；保留字段兼容
	TotalPlayTime       float64 `gorm:"default:0" json:"total_play_time"`
	CategoryPermissions string  `gorm:"type:text;default:'[]'" json:"category_permissions"`
	Permissions         string  `gorm:"type:text;default:'[]'" json:"permissions"`
	AllowedPlayTime     string  `gorm:"type:text;default:''" json:"allowed_play_time"`
	IsMuted             int     `gorm:"default:0" json:"is_muted"`
}

// MarshalJSON 为玩家对象注入 account_type（虚拟字段，不入库；供前端区分账号体系）
func (u User) MarshalJSON() ([]byte, error) {
	type plain User
	return json.Marshal(struct {
		plain
		AccountType string `json:"account_type"`
	}{plain(u), AccountTypePlayer})
}

// SysManager 管理员模型（独立账号体系）
// ManagerCode：对外展示的管理员编码，超管固定 G00，其余 G01 起自增
type SysManager struct {
	ID                  uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ManagerCode         string `gorm:"type:text;unique;index" json:"manager_code"` // G00 / G01 ...
	Username            string `gorm:"unique;not null" json:"username"`
	Password            string `gorm:"not null" json:"-"`
	Nickname            string `gorm:"type:text" json:"nickname"`
	Avatar              string `gorm:"type:text" json:"avatar"`
	Role                string `gorm:"type:text" json:"role"` // super_admin / admin
	Permissions         string `gorm:"type:text;default:'[]'" json:"permissions"`
	CategoryPermissions string `gorm:"type:text;default:'[]'" json:"category_permissions"`
	IsEnabled           int    `gorm:"default:1" json:"is_enabled"` // 1=启用 0=禁用（撤销权限）
	AllowRank           int    `gorm:"default:0" json:"allow_rank"` // 管理员游玩是否计入榜单（默认否）
	CreatedAt           int64  `json:"created_at"`
}

// MarshalJSON 为管理员对象注入 account_type（虚拟字段，不入库；供前端区分账号体系）
func (m SysManager) MarshalJSON() ([]byte, error) {
	type plain SysManager
	return json.Marshal(struct {
		plain
		AccountType string `json:"account_type"`
	}{plain(m), AccountTypeManager})
}

// UserManagerBind 人员绑定：把同一人的玩家账号与管理员账号关联，支持免密切换
type UserManagerBind struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	PlayerUID   string `gorm:"type:text;index" json:"player_uid"`     // 玩家编码 0000005
	ManagerCode string `gorm:"type:text;index" json:"manager_code"`   // 管理员编码 G04
	CreatedAt   int64  `json:"created_at"`
}

// Game 游戏模型
type Game struct {
	ID          uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string  `gorm:"type:text" json:"name"`
	CoverPath   string  `gorm:"type:text" json:"cover_path"`
	MountPath   string  `gorm:"type:text" json:"mount_path"`
	CategoryID  uint    `json:"category_id"` // 兼容旧字段：= CategoryIDs 首项
	SortOrder   int     `gorm:"default:0" json:"sort_order"`
	IsHidden    int     `gorm:"default:0" json:"is_hidden"`
	IsLandscape int     `gorm:"default:0" json:"is_landscape"`
	Tag         string  `gorm:"type:text;default:'default'" json:"tag"` // 兼容旧字段：= Tags 首项
	PlatformTag string  `gorm:"type:text" json:"platform_tag"`          // 平台标签（按文件类型自动识别，如 红白机 NES / 任天堂 GBA / HTML5）
	GameType    string  `gorm:"type:text;default:'H5'" json:"game_type"`
	HotTime     float64 `gorm:"default:0" json:"hot_time"`
	ContentHash string  `gorm:"type:text;index" json:"content_hash"` // 导入内容指纹（去重判据）
	Aliases     string  `gorm:"type:text" json:"aliases"`             // 别名（曾用名），JSON 数组字符串，如 ["玩家国度","开店我最牛"]
	// 刮削元数据（可选，由元数据刮削任务写入）
	Description    string `gorm:"type:text" json:"description"`     // 游戏简介
	ReleaseDate    string `gorm:"type:text" json:"release_date"`    // 发行日期（字符串，来源各异）
	ExternalSource string `gorm:"type:text" json:"external_source"` // 元数据来源（如 libretro）
	CreatedAt      int64  `json:"created_at"`

	// 统计字段（用于首页排序/评分展示）
	SizeBytes   int64   `gorm:"default:0" json:"size_bytes"`   // 游戏文件占用字节数（按大小排序）
	AvgRating   float64 `gorm:"default:0" json:"avg_rating"`   // 综合评分（缓存，写评分时聚合更新）
	RatingCount int     `gorm:"default:0" json:"rating_count"` // 评分人数（缓存）

	// 多值聚合字段（不落库，来自关联表）
	CategoryIDs []uint   `gorm:"-" json:"category_ids"`
	Tags        []string `gorm:"-" json:"tags"`
}

// GameCategory 游戏↔分类 多对多关联表
type GameCategory struct {
	GameID     uint `gorm:"primaryKey;index" json:"game_id"`
	CategoryID uint `gorm:"primaryKey;index" json:"category_id"`
}

// GameTag 游戏↔标签 多对多关联表
type GameTag struct {
	GameID uint   `gorm:"primaryKey;index" json:"game_id"`
	Tag    string `gorm:"primaryKey;type:text" json:"tag"`
}

// GameRating 游戏评分（一人一票，可修改）
// RaterKey 为评分者账号编码（玩家 0000001 / 管理员 G00），跨两种账号体系唯一
type GameRating struct {
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	GameID    uint   `gorm:"uniqueIndex:idx_rating_game_rater" json:"game_id"`
	RaterKey  string `gorm:"type:text;uniqueIndex:idx_rating_game_rater" json:"rater_key"`
	Score     int    `json:"score"` // 1..5 星
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Category 游戏分类模型
type Category struct {
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string `gorm:"type:text" json:"name"`
	SortOrder int    `gorm:"default:0" json:"sort_order"`
}

// Setting 系统设置键值对
type Setting struct {
	Key   string `gorm:"primaryKey" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint   `json:"user_id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	Avatar      string `json:"avatar"`
	Content     string `json:"content"`
	Timestamp   int64  `json:"timestamp"`
	IsManager   int    `gorm:"default:0" json:"is_manager"`   // 1=管理员发言（前端展示标识）
	Role        string `gorm:"type:text" json:"role"`         // 发言者角色（user/admin/super_admin）
	AccountCode string `gorm:"type:text" json:"account_code"` // 发言者账号编码（0000001 / G00）
}

// UserPreference 用户偏好（如 cloud_save_localstorage）
type UserPreference struct {
	UserID    uint   `gorm:"primaryKey" json:"user_id"`
	Key       string `gorm:"primaryKey" json:"key"`
	Value     string `gorm:"type:text" json:"value"`
	UpdatedAt int64  `json:"updated_at"`
}

// UserSaveBackup 用户存档备份
type UserSaveBackup struct {
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint   `gorm:"index" json:"user_id"`
	Title     string `gorm:"type:text" json:"title"`
	Data      string `gorm:"type:text" json:"data"`
	CreatedAt int64  `json:"created_at"`
}

// Claims JWT 载荷
// AccountType 区分玩家/管理员（player/manager）；AccountCode 为对外编码（0000001 / G00）
type Claims struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	AccountType string `json:"account_type"`
	AccountCode string `json:"account_code"`
	jwt.RegisteredClaims
}
