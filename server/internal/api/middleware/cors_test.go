package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/config"
)

// newCORSRouter 构造仅启用 CORS 中间件的测试路由
func newCORSRouter(origins []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORSMiddleware(&config.Config{CORSOrigins: origins}))
	r.GET("/api/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

// TestCORSAllowedOrigin 白名单来源应回显 Origin + 允许凭据
func TestCORSAllowedOrigin(t *testing.T) {
	r := newCORSRouter([]string{"https://app.example.com"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("Origin", "https://app.example.com")
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Allow-Origin = %q, 期望回显来源", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, 期望 true", got)
	}
}

// TestCORSDisallowedOrigin 非白名单来源不应回显跨域头
func TestCORSDisallowedOrigin(t *testing.T) {
	r := newCORSRouter([]string{"https://app.example.com"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("非白名单来源不应回显 Allow-Origin, 实际 %q", got)
	}
}

// TestCORSPreflight OPTIONS 预检应返回 204 且带允许方法与头
func TestCORSPreflight(t *testing.T) {
	r := newCORSRouter([]string{"https://app.example.com"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/ping", nil)
	req.Header.Set("Origin", "https://app.example.com")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("预检状态码 = %d, 期望 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Errorf("预检应带 Access-Control-Allow-Headers")
	}
}

// TestCORSNoOrigin 无 Origin（同源）应正常放行且不加跨域头
func TestCORSNoOrigin(t *testing.T) {
	r := newCORSRouter([]string{"https://app.example.com"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("同源请求状态码 = %d, 期望 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("同源请求不应加 Allow-Origin, 实际 %q", got)
	}
}
