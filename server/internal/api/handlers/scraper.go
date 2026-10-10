package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// 元数据 / 图片刮削模块
// 设计（轻量自研，对标 RomM 的 provider 体系但大幅简化）：
//   - MetadataProvider 接口：Name() / Enabled() / Search()；集中注册为切片
//   - 逐字段优先级回退：高优先级已填字段不被低优先级覆盖（claimed 集合）
//   - 首个免密 provider：libretro（实现见 scraper_libretro.go）
//   - 封面复用现有落盘链（cover.<ext>），写回 Game.CoverPath
//   - 网络可配置：代理 / 超时走系统设置，便于受限网络环境

// ScrapedMetadata provider 返回的标准化元数据（各字段可空）
type ScrapedMetadata struct {
	Name        string
	Description string
	ReleaseDate string
	CoverURL    string
	Provider    string
}

// MetadataProvider 元数据源接口：新增源只需实现接口并注册
type MetadataProvider interface {
	// Name 源标识（用于配置与日志）
	Name() string
	// Enabled 自报是否可用（凭证是否配置 / 源是否开启），扫描时过滤
	Enabled() bool
	// Search 按名称与平台标签检索候选元数据；未命中返回 nil, nil
	Search(name, platformTag string) (*ScrapedMetadata, error)
}

// metadataProviders provider 注册表（集中注册，便于编排遍历）
var metadataProviders = []MetadataProvider{
	&libretroProvider{},
}

// ---------- 刮削网络配置 ----------

// 刮削网络设置键：scrape_proxy（代理地址）、scrape_timeout_sec（单请求超时秒数）
const (
	settingScrapeProxy    = "scrape_proxy"
	settingScrapeTimeout  = "scrape_timeout_sec"
	defaultScrapeTimeout  = 60
	minScrapeTimeout      = 5
	maxScrapeTimeout      = 300
)

var (
	scrapeClientMu    sync.Mutex
	scrapeClientCache *http.Client
	scrapeClientSig   string
)

// scrapeClient 返回按系统设置构建的刮削 HTTP 客户端。
//   - 代理：设置项 scrape_proxy（如 http://127.0.0.1:7890）；未配置时回退到
//     环境变量 HTTP_PROXY / HTTPS_PROXY（Go 不读取 Windows 系统代理，需显式配置）
//   - 超时：设置项 scrape_timeout_sec（默认 60s，覆盖整个请求含响应体读取）
//
// 设置变更（签名变化）时自动重建客户端，无需重启服务。
func scrapeClient() *http.Client {
	proxy := strings.TrimSpace(getSettingValue(settingScrapeProxy, ""))
	timeout := settingInt(settingScrapeTimeout, defaultScrapeTimeout, minScrapeTimeout, maxScrapeTimeout)
	sig := fmt.Sprintf("%s|%d", proxy, timeout)

	scrapeClientMu.Lock()
	defer scrapeClientMu.Unlock()
	if scrapeClientCache != nil && scrapeClientSig == sig {
		return scrapeClientCache
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        16,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil && pu.Host != "" {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	scrapeClientCache = &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: tr,
	}
	scrapeClientSig = sig
	return scrapeClientCache
}

// settingInt 读取整型设置项，缺失 / 非法 / 越界时返回默认值
func settingInt(key string, def, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(getSettingValue(key, "")))
	if err != nil || n < min || n > max {
		return def
	}
	return n
}

// headOK 以 HEAD 探测 URL 是否存在（2xx）
func headOK(u string) (bool, error) {
	req, err := http.NewRequest(http.MethodHead, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := scrapeClient().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// coverByURL HEAD 校验通过后返回封面元数据
func coverByURL(u, provider string) (*ScrapedMetadata, bool) {
	ok, err := headOK(u)
	if err != nil || !ok {
		return nil, false
	}
	return &ScrapedMetadata{CoverURL: u, Provider: provider}, true
}

// ---------- 逐字段优先级回退编排 ----------

// ScrapeResult 刮削结果（回传前端展示）
type ScrapeResult struct {
	CoverSet    bool     `json:"cover_set"`
	Description string   `json:"description,omitempty"`
	ReleaseDate string   `json:"release_date,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Missed      bool     `json:"missed"`
	Errors      []string `json:"errors,omitempty"`
}

// scrapeGame 逐字段优先级回退：按 provider 顺序，高优先级已填字段不被覆盖
func scrapeGame(game *models.Game) ScrapeResult {
	claimed := map[string]bool{}
	merged := &ScrapedMetadata{}
	var errs []string

	for _, p := range metadataProviders {
		if !p.Enabled() {
			continue
		}
		md, err := p.Search(game.Name, game.PlatformTag)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p.Name(), err))
			continue
		}
		if md == nil {
			continue
		}
		// 逐字段合并：先到先得（高优先级已 claimed 的字段跳过）
		if !claimed["description"] && md.Description != "" {
			merged.Description = md.Description
			claimed["description"] = true
		}
		if !claimed["release_date"] && md.ReleaseDate != "" {
			merged.ReleaseDate = md.ReleaseDate
			claimed["release_date"] = true
		}
		if !claimed["cover"] && md.CoverURL != "" {
			merged.CoverURL = md.CoverURL
			claimed["cover"] = true
		}
		if merged.Provider == "" && md.Provider != "" {
			merged.Provider = md.Provider
		}
	}

	res := ScrapeResult{
		Description: merged.Description,
		ReleaseDate: merged.ReleaseDate,
		Provider:    merged.Provider,
		Errors:      errs,
	}
	if merged.Description == "" && merged.ReleaseDate == "" && merged.CoverURL == "" {
		res.Missed = true
		return res
	}

	// 落库：仅在字段为空时写入（不覆盖用户手工内容）
	if merged.Description != "" && game.Description == "" {
		game.Description = merged.Description
	}
	if merged.ReleaseDate != "" && game.ReleaseDate == "" {
		game.ReleaseDate = merged.ReleaseDate
	}
	if merged.CoverURL != "" && game.CoverPath == "" {
		path, ok := downloadCoverToGame(game, merged.CoverURL)
		if ok {
			game.CoverPath = path
			res.CoverSet = true
		} else {
			res.Errors = append(res.Errors, "封面下载失败")
		}
	}
	if merged.Provider != "" {
		game.ExternalSource = merged.Provider
	}
	database.DB.Save(game)
	return res
}

// downloadCoverToGame 下载封面并落盘到游戏目录，返回绝对路径
func downloadCoverToGame(game *models.Game, coverURL string) (string, bool) {
	req, err := http.NewRequest(http.MethodGet, coverURL, nil)
	if err != nil {
		return "", false
	}
	resp, err := scrapeClient().Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	// 体积上限保护（+1 用于探测超限）
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxCoverBytes {
		return "", false
	}
	// 扩展名从 URL 推导（不信任响应），默认 .png
	ext := imageExtFor(mimeFromURL(coverURL))
	base := resolveGamePath(game.MountPath)
	if base == "" {
		base = filepath.Join(gamesDir(), sanitizeDirName(game.Name))
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", false
	}
	dst := filepath.Join(base, "cover"+ext)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", false
	}
	return dst, true
}

// mimeFromURL 从 URL 扩展名推断 MIME（供 imageExtFor 使用）
func mimeFromURL(u string) string {
	low := toLower(u)
	switch {
	case strings.HasSuffix(low, ".jpg"), strings.HasSuffix(low, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(low, ".webp"):
		return "image/webp"
	case strings.HasSuffix(low, ".gif"):
		return "image/gif"
	default:
		return "image/png"
	}
}

// dedupeStrings 去重并保持顺序
func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ---------- HTTP handler ----------

// ScrapeGame 刮削单个游戏元数据 / 封面
// POST /api/games/:id/scrape（需 game.manage 权限）
func ScrapeGame(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var game models.Game
	if err := database.DB.First(&game, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}
	res := scrapeGame(&game)
	c.JSON(http.StatusOK, gin.H{"message": "刮削完成", "result": res, "game": game})
}