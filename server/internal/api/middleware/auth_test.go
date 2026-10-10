package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/config"
)

// 断言：普通鉴权失败返回 401
func TestAuthMiddleware_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{AuthCookie: "uogame_token", JWTSecret: "test-secret"}
	r := gin.New()
	r.GET("/x", AuthMiddleware(cfg), func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("AuthMiddleware 未授权应返回 401，实际 %d", w.Code)
	}
}

// 断言：隐藏资源鉴权失败掩码为 404（不暴露资源是否存在）
func TestHiddenAuthMiddleware_MasksAsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{AuthCookie: "uogame_token", JWTSecret: "test-secret"}
	r := gin.New()
	r.GET("/x", HiddenAuthMiddleware(cfg), func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("HiddenAuthMiddleware 未授权应掩码为 404，实际 %d", w.Code)
	}
}

// 断言：携带非法 token 时同样掩码为 404（防伪造探测）
func TestHiddenAuthMiddleware_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{AuthCookie: "uogame_token", JWTSecret: "test-secret"}
	r := gin.New()
	r.GET("/x", HiddenAuthMiddleware(cfg), func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x?token=invalid.jwt.token", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("非法 token 应掩码为 404，实际 %d", w.Code)
	}
}