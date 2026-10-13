package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"uogame-go/internal/config"
	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// tokenTTL JWT 有效期（同时用于 Cookie MaxAge）
const tokenTTL = 7 * 24 * time.Hour

// GetTokenFromRequest 从请求中提取 token：优先 Cookie，其次 Authorization 头，再次查询参数
func GetTokenFromRequest(c *gin.Context, cookieName string) string {
	// 1. HttpOnly Cookie
	if cookieName != "" {
		if v, err := c.Cookie(cookieName); err == nil && v != "" {
			return v
		}
	}
	// 2. Authorization: Bearer xxx
	if h := c.GetHeader("Authorization"); h != "" {
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return strings.TrimSpace(h)
	}
	// 3. 查询参数 token
	if t := c.Query("token"); t != "" {
		return t
	}
	return ""
}

// ParseToken 校验并解析 JWT
func ParseToken(tokenStr, secret string) (*models.Claims, error) {
	claims := &models.Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// SetAuthCookie 下发认证 Cookie
func SetAuthCookie(c *gin.Context, cfg *config.Config, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cfg.AuthCookie, token, int(tokenTTL.Seconds()), "/", "", false, true)
}

// ClearAuthCookie 清除认证 Cookie
func ClearAuthCookie(c *gin.Context, cfg *config.Config) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cfg.AuthCookie, "", -1, "/", "", false, true)
}

// maybeRefreshAuthToken 剩余有效期不足一半时自动续签
func maybeRefreshAuthToken(c *gin.Context, cfg *config.Config, claims *models.Claims) {
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return
	}
	if time.Until(exp.Time) > tokenTTL/2 {
		return
	}
	if token, err := GenerateToken(claims.ID, claims.Username, claims.Role, claims.AccountType, claims.AccountCode, cfg.JWTSecret); err == nil {
		SetAuthCookie(c, cfg, token)
	}
}

// GenerateToken 生成签名 JWT
// accountType：player / manager；accountCode：0000001 / G00
func GenerateToken(id uint, username, role, accountType, accountCode, secret string) (string, error) {
	now := time.Now()
	if accountType == "" {
		accountType = models.AccountTypePlayer
	}
	claims := &models.Claims{
		ID:          id,
		Username:    username,
		Role:        role,
		AccountType: accountType,
		AccountCode: accountCode,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// AuthMiddleware 鉴权中间件：校验 token，注入上下文
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return authMiddleware(cfg, http.StatusUnauthorized, "Unauthorized")
}

// HiddenAuthMiddleware 隐藏资源鉴权中间件：鉴权失败统一返回 404，避免暴露资源是否存在
// 用于 /play/* 游戏文件路由，防止通过状态码（401/403）枚举探测游戏与文件
func HiddenAuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return authMiddleware(cfg, http.StatusNotFound, "Not found")
}

// authMiddleware 鉴权中间件实现（failStatus/failMsg 决定失败时的响应，便于掩码复用）
func authMiddleware(cfg *config.Config, failStatus int, failMsg string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := GetTokenFromRequest(c, cfg.AuthCookie)
		if tokenStr == "" {
			c.AbortWithStatusJSON(failStatus, gin.H{"error": failMsg})
			return
		}
		claims, err := ParseToken(tokenStr, cfg.JWTSecret)
		if err != nil {
			c.AbortWithStatusJSON(failStatus, gin.H{"error": failMsg})
			return
		}
		maybeRefreshAuthToken(c, cfg, claims)
		accountType := claims.AccountType
		if accountType == "" {
			accountType = models.AccountTypePlayer // 兼容旧 token
		}
		c.Set("userID", claims.ID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Set("accountType", accountType)
		c.Set("accountCode", claims.AccountCode)
		c.Next()
	}
}

// CurrentAccountType 读取当前请求的账号体系（player/manager），默认 player
func CurrentAccountType(c *gin.Context) string {
	if v, ok := c.Get("accountType"); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return models.AccountTypePlayer
}

// IsManagerRequest 当前请求是否为管理员身份
func IsManagerRequest(c *gin.Context) bool {
	return CurrentAccountType(c) == models.AccountTypeManager
}

// AdminMiddleware 管理员中间件（仅管理员账号可访问，依赖 AuthMiddleware）
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsManagerRequest(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Admin permission required"})
			return
		}
		role, _ := c.Get("role")
		if r, _ := role.(string); r != models.RoleAdmin && r != models.RoleSuperAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Admin permission required"})
			return
		}
		c.Next()
	}
}

// RequirePermission 按权限点鉴权（依赖 AuthMiddleware 先执行）
//   - 仅管理员体系账号可命中；玩家体系一律拒绝
//   - super_admin：放行全部
//   - admin：回查 sys_manager 的 Permissions 字段（避免旧 token 权限过期）
//   - 兼容：admin 角色若 Permissions 为空，视为拥有全部权限（升级前老账号）
func RequirePermission(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 玩家体系不参与后台权限
		if !IsManagerRequest(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Permission denied: " + perm})
			return
		}
		role, _ := c.Get("role")
		if r, _ := role.(string); r == models.RoleSuperAdmin {
			c.Next()
			return
		}
		if HasPermission(c, perm) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Permission denied: " + perm})
	}
}

// HasPermission 判断当前登录管理员是否拥有指定权限点（回查 sys_manager）
func HasPermission(c *gin.Context, perm string) bool {
	userID, ok := c.Get("userID")
	if !ok {
		return false
	}
	id, ok := userID.(uint)
	if !ok {
		return false
	}
	var mgr models.SysManager
	if err := database.DB.Select("role", "permissions", "is_enabled").First(&mgr, id).Error; err != nil {
		return false
	}
	if mgr.IsEnabled == 0 {
		return false
	}
	if mgr.Role == models.RoleSuperAdmin {
		return true
	}
	if mgr.Role != models.RoleAdmin {
		return false
	}
	// admin：按勾选的权限点放行；未勾选（空）即无任何管理权限（最小权限原则）
	perms := ParsePermissions(mgr.Permissions)
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// ParsePermissions 解析 Permissions JSON 数组（容错：非法/空 → 空切片）
func ParsePermissions(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
