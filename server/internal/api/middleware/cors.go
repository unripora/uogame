package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/config"
)

// CORSMiddleware 跨域中间件（游戏源模式）
//
// 策略：
//   - 仅对「已配置白名单」的来源放行（CORS_ORIGINS，逗号分隔）；
//   - 未配置白名单时：对同源请求正常放行，不回显任何跨域头（等价不开放跨域）；
//   - 携带凭据（Cookie）时，Access-Control-Allow-Origin 必须回显具体来源，禁止通配 `*`；
//   - 允许 Authorization 头（源模式前端跨域走 Bearer），处理 OPTIONS 预检。
func CORSMiddleware(cfg *config.Config) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]bool, len(cfg.CORSOrigins))
	for _, o := range cfg.CORSOrigins {
		if o == "*" {
			allowAll = true
			continue
		}
		allowed[strings.ToLower(o)] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		// 无 Origin（同源/非浏览器）不处理跨域头，直接放行
		if origin == "" {
			c.Next()
			return
		}

		if isOriginAllowed(origin, allowed, allowAll) {
			h := c.Writer.Header()
			// 带凭据场景必须回显具体 Origin，不能使用 *
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "86400")
		}

		// 预检请求直接返回 204（无论是否放行，避免进入业务路由）
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// isOriginAllowed 判断来源是否在白名单内
func isOriginAllowed(origin string, allowed map[string]bool, allowAll bool) bool {
	if allowAll {
		return true
	}
	return allowed[strings.ToLower(origin)]
}
