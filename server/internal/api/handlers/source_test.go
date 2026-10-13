package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// seedSourceGame 插入一条用于源接口测试的游戏
func seedSourceGame(t *testing.T, name, gameType string, hidden int) *models.Game {
	t.Helper()
	g := &models.Game{Name: name, GameType: gameType, IsHidden: hidden, Tag: "default"}
	if err := database.DB.Create(g).Error; err != nil {
		t.Fatalf("插入测试游戏失败: %v", err)
	}
	return g
}

// TestSourceEndpoints 通过 gin 引擎测试源描述 / 清单 / 搜索 / 详情
func TestSourceEndpoints(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	seedSourceGame(t, "神兽战场", "H5", 0)
	seedSourceGame(t, "荒岛求生HD", "H5", 0)
	seedSourceGame(t, "隐藏游戏", "H5", 1) // 不应出现在清单

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/.well-known/uogame-source.json", SourceWellKnown)
	r.GET("/api/source/manifest", SourceManifest)
	r.GET("/api/source/search", SourceSearch)
	r.GET("/api/source/game/:id", SourceGameDetail)

	// 1) 源描述
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/uogame-source.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("源描述状态码 = %d, 期望 200", rec.Code)
	}
	var desc sourceDescriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &desc); err != nil {
		t.Fatalf("解析源描述失败: %v", err)
	}
	if desc.Protocol != sourceProtocol || desc.Version != sourceVersion {
		t.Errorf("协议标识/版本不符: %+v", desc)
	}
	if desc.Endpoints.Manifest == "" || desc.Endpoints.Stream == "" {
		t.Errorf("端点缺失: %+v", desc.Endpoints)
	}

	// 2) 清单：只含未隐藏的 2 条
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/source/manifest?page=1&page_size=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("清单状态码 = %d, 期望 200", rec.Code)
	}
	var manifest struct {
		Items    []SourceGame `json:"items"`
		Total    int64        `json:"total"`
		Page     int          `json:"page"`
		PageSize int          `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("解析清单失败: %v", err)
	}
	// 测试库为共享内存库，可能残留其它用例数据：只断言「本次种子」的 2 条都在，隐藏的不在
	found := map[string]bool{}
	for _, it := range manifest.Items {
		found[it.Name] = true
	}
	if !found["神兽战场"] || !found["荒岛求生HD"] {
		t.Fatalf("清单应含种子的 2 条游戏, 实际 items=%d", len(manifest.Items))
	}
	for _, it := range manifest.Items {
		if it.Name == "隐藏游戏" {
			t.Fatalf("隐藏游戏不应出现在清单")
		}
	}

	// 3) 搜索：命中「神兽」
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/source/search?q=神兽", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("搜索状态码 = %d, 期望 200", rec.Code)
	}
	var search struct {
		Items []SourceGame `json:"items"`
		Total int64        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &search); err != nil {
		t.Fatalf("解析搜索结果失败: %v", err)
	}
	// 只断言关键词命中结果均匹配（共享库可能含其它数据）
	for _, it := range search.Items {
		if it.Name != "神兽战场" {
			t.Errorf("搜索「神兽」不应命中: %s", it.Name)
		}
	}
	hit := false
	for _, it := range search.Items {
		if it.Name == "神兽战场" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("搜索「神兽」未命中「神兽战场」")
	}

	// 4) 详情：命中 / 不存在返回 404
	id := manifest.Items[0].ID
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/source/game/"+strconv.FormatUint(uint64(id), 10), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("详情状态码 = %d, 期望 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/source/game/999999", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在游戏详情状态码 = %d, 期望 404", rec.Code)
	}
}
