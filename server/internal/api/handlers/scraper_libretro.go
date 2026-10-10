package handlers

import (
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---------- libretro 免密 provider ----------
//
// 图库地址：https://thumbnails.libretro.com/<Platform>/Named_Boxarts/<Name>.png
// 命名习惯为「游戏名 + (区域)」，如 1942 (Japan, USA).png / Tekken (USA).png，
// 与库内常见的简名（1942 / Tekken）不同，故采用两级匹配：
//  1. 轻量候选名探测：原名 / 去括号后缀 / 追加常见区域后缀（HEAD 探测，快）
//  2. 目录索引模糊匹配：拉取 Apache 目录索引（按平台缓存 24h），
//     归一化去括号标签后做精确 / Jaro-Winkler(≥0.92) 匹配（准）
// 索引已缓存时直接走第 2 级，避免重复拉取数 MB 的索引；未缓存时先走第 1 级，
// 命中即返回，避免为单个游戏付出拉取大索引的代价。

// libretroProvider 基于 thumbnails.libretro.com 的免密封面源
type libretroProvider struct{}

func (p *libretroProvider) Name() string { return "libretro" }

func (p *libretroProvider) Enabled() bool { return true } // 免密，始终可用

// libretroPlatformByTag 平台标签码 → libretro 平台目录名（仅覆盖已知 ROM 平台）
var libretroPlatformByTag = map[string]string{
	"nes":       "Nintendo - Nintendo Entertainment System",
	"gb":        "Nintendo - Game Boy",
	"gba":       "Nintendo - Game Boy Advance",
	"snes":      "Nintendo - Super Nintendo Entertainment System",
	"n64":       "Nintendo - Nintendo 64",
	"nds":       "Nintendo - Nintendo DS",
	"segaMD":    "Sega - Mega Drive - Genesis",
	"sega32x":   "Sega - 32X",
	"segaMS":    "Sega - Master System - Mark III",
	"segaGG":    "Sega - Game Gear",
	"pce":       "NEC - PC Engine - TurboGrafx 16",
	"ngp":       "SNK - Neo Geo Pocket",
	"ws":        "Bandai - WonderSwan",
	"vb":        "Nintendo - Virtual Boy",
	"psx":       "Sony - PlayStation",
	"atari2600": "Atari - 2600",
	"atari5200": "Atari - 5200",
	"atari7800": "Atari - 7800",
	"coleco":    "Coleco - ColecoVision",
}

// platformTagCodeFromLabel 将中文平台标签（如「任天堂 GBA」）还原为平台码（如 gba）
func platformTagCodeFromLabel(label string) string {
	for code, lbl := range platformTagLabels {
		if lbl == label {
			return code
		}
	}
	return ""
}

// libretroRegions 常见区域后缀（按命中概率排序），用于生成候选名称
var libretroRegions = []string{
	"USA", "Europe", "Japan", "World",
	"Japan, USA", "USA, Europe", "Japan, Europe", "USA, Japan",
}

// libretroMatchThreshold 索引模糊匹配阈值（归一化名称的 Jaro-Winkler 相似度）
const libretroMatchThreshold = 0.92

// libretroBoxartBase 某平台 Named_Boxarts 目录的 URL 前缀
func libretroBoxartBase(platform string) string {
	return "https://thumbnails.libretro.com/" + url.PathEscape(platform) + "/Named_Boxarts/"
}

// Search 实现 MetadataProvider：两级匹配（候选名探测 → 目录索引模糊匹配）
func (p *libretroProvider) Search(name, platformTag string) (*ScrapedMetadata, error) {
	platform := libretroPlatformByTag[platformTagCodeFromLabel(platformTag)]
	if platform == "" {
		// 未识别的平台（如 HTML5/Flash），libretro 无对应图库
		return nil, nil
	}
	base := libretroBoxartBase(platform)

	// 索引已缓存 → 直接索引匹配（准确，且无需重复拉取大索引）
	if names := libretroCachedNames(platform); names != nil {
		if href := libretroPickBest(name, names); href != "" {
			if md, ok := coverByURL(base+href, p.Name()); ok {
				return md, nil
			}
		}
		return nil, nil
	}

	// 索引未缓存 → 轻量候选名探测，命中即返回
	for _, cand := range libretroNameCandidates(name) {
		if md, ok := coverByURL(base+url.PathEscape(cand)+".png", p.Name()); ok {
			return md, nil
		}
	}

	// 仍未命中 → 拉取目录索引做模糊匹配（成功后同平台后续游戏走缓存）
	if names := libretroFetchNames(platform); names != nil {
		if href := libretroPickBest(name, names); href != "" {
			if md, ok := coverByURL(base+href, p.Name()); ok {
				return md, nil
			}
		}
	}
	return nil, nil
}

// libretroNameCandidates 生成候选名称：原名 / 去括号后缀 / 追加常见区域后缀
func libretroNameCandidates(name string) []string {
	base := strings.TrimSpace(name)
	if base == "" {
		return nil
	}
	// 去掉结尾的 (...) / [...] 版本/区域后缀得到主名
	stem := base
	if i := strings.LastIndexAny(base, "(["); i > 0 {
		if t := strings.TrimSpace(base[:i]); t != "" {
			stem = t
		}
	}
	out := []string{base}
	if stem != base {
		out = append(out, stem)
	}
	for _, r := range libretroRegions {
		out = append(out, stem+" ("+r+")")
	}
	return dedupeStrings(out)
}

// ---------- 目录索引缓存 ----------

const (
	libretroIndexTTL     = 24 * time.Hour
	maxLibretroIndexSize = 16 << 20 // 单个平台索引体积上限（NES 约 4MB）
)

// libretroIndex 单平台的封面文件清单缓存
type libretroIndex struct {
	fetched time.Time
	hrefs   []string // 索引中的原始（URL 转义）文件链接
}

var (
	libretroIndexMu    sync.Mutex
	libretroIndexCache = map[string]*libretroIndex{}
)

// libretroCachedNames 返回未过期的索引缓存；无缓存返回 nil
func libretroCachedNames(platform string) []string {
	libretroIndexMu.Lock()
	defer libretroIndexMu.Unlock()
	if idx, ok := libretroIndexCache[platform]; ok && time.Since(idx.fetched) < libretroIndexTTL {
		return idx.hrefs
	}
	return nil
}

// libretroFetchNames 拉取并缓存平台目录索引；失败返回 nil（调用方降级处理）
func libretroFetchNames(platform string) []string {
	req, err := http.NewRequest(http.MethodGet, libretroBoxartBase(platform), nil)
	if err != nil {
		return nil
	}
	resp, err := scrapeClient().Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLibretroIndexSize+1))
	if err != nil || len(body) == 0 || len(body) > maxLibretroIndexSize {
		return nil
	}
	names := parseLibretroIndex(string(body))
	if len(names) == 0 {
		return nil
	}
	libretroIndexMu.Lock()
	libretroIndexCache[platform] = &libretroIndex{fetched: time.Now(), hrefs: names}
	libretroIndexMu.Unlock()
	return names
}

// libretroHrefRe 匹配 Apache 目录索引中的 .png 文件链接
var libretroHrefRe = regexp.MustCompile(`href="([^"]+\.png)"`)

// parseLibretroIndex 从目录索引 HTML 提取 .png 文件链接（过滤父目录 / 排序链接）
func parseLibretroIndex(html string) []string {
	matches := libretroHrefRe.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		raw := m[1]
		if strings.HasPrefix(raw, "/") || strings.Contains(raw, "?") {
			continue
		}
		out = append(out, raw)
	}
	return out
}

// ---------- 名称归一化与模糊匹配 ----------

// libretroTagRe 匹配名称中的 (...) / [...] 标签段
var libretroTagRe = regexp.MustCompile(`[\(\[][^\)\]]*[\)\]]`)

// libretroNormalize 归一化名称用于比对：
// 去 .png 扩展名 → 去括号标签 → 小写 → 保留字母数字与 CJK，其余折算为空格
func libretroNormalize(s string) string {
	s = strings.TrimSpace(s)
	if ext := filepath.Ext(s); strings.EqualFold(ext, ".png") {
		s = strings.TrimSuffix(s, ext)
	}
	s = libretroTagRe.ReplaceAllString(s, " ")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127:
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// libretroPickBest 在索引链接中挑选与查询名最匹配的一项，返回其原始链接；无命中返回空串
func libretroPickBest(query string, hrefs []string) string {
	q := libretroNormalize(query)
	if q == "" {
		return ""
	}
	best := ""
	var bestScore float64
	var bestQ [5]int
	for _, h := range hrefs {
		dec, err := url.PathUnescape(h)
		if err != nil {
			continue
		}
		c := libretroNormalize(dec)
		if c == "" {
			continue
		}
		sc := libretroScore(q, c)
		if sc < libretroMatchThreshold {
			continue
		}
		qv := libretroQuality(dec)
		// 归一化后同名条目极多（区域 / 版本 / 发行商 / dump 标签各异）：
		// 相似度相同时按「典型度」择优，避免选中 PlayChoice-10、[b] 等冷门版本
		if best == "" || sc > bestScore || (sc == bestScore && lessQuality(qv, bestQ)) {
			best, bestScore, bestQ = h, sc, qv
		}
	}
	return best
}

// lessQuality 按典型度比较两个索引条目（字典序，越小越优）
func lessQuality(a, b [5]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// libretroRegionTokens 括号内被视为「区域 / 语言」的合法片段，其余视为版本 / 发行商等修饰
var libretroRegionTokens = map[string]bool{
	"usa": true, "us": true, "europe": true, "eu": true, "japan": true, "jp": true,
	"world": true, "asia": true, "korea": true, "kr": true, "china": true, "cn": true,
	"taiwan": true, "tw": true, "hong kong": true, "hk": true, "brazil": true, "br": true,
	"spain": true, "france": true, "germany": true, "italy": true, "netherlands": true,
	"australia": true, "canada": true, "uk": true, "sweden": true, "russia": true,
	"en": true, "ja": true, "fr": true, "de": true, "es": true, "it": true, "nl": true,
	"pt": true, "sv": true, "zh": true, "ko": true, "ru": true,
}

// libretroGroupRe 提取名称中的圆括号分组
var libretroGroupRe = regexp.MustCompile(`\(([^)]*)\)`)

// libretroQuality 评估索引条目的「典型度」，越小越优：
// (方括号标签数, 非区域分组数, 常见区域排序, 分组总数, 名称长度)
func libretroQuality(dec string) [5]int {
	dec = strings.TrimSuffix(dec, filepath.Ext(dec))
	low := strings.ToLower(dec)

	regionRank := len(libretroRegions) // 无常见区域标记
	for i, r := range libretroRegions {
		if strings.Contains(low, "("+strings.ToLower(r)+")") {
			regionRank = i
			break
		}
	}
	groups, nonRegion := 0, 0
	for _, g := range libretroGroupRe.FindAllStringSubmatch(low, -1) {
		groups++
		if !isRegionGroup(g[1]) {
			nonRegion++
		}
	}
	return [5]int{strings.Count(dec, "["), nonRegion, regionRank, groups, len(dec)}
}

// isRegionGroup 判断括号内容是否纯为区域 / 语言片段（支持「Japan, USA」这类逗号组合）
func isRegionGroup(content string) bool {
	parts := strings.Split(content, ",")
	for _, p := range parts {
		if !libretroRegionTokens[strings.TrimSpace(p)] {
			return false
		}
	}
	return true
}

// libretroScore 归一化名称相似度：完全相同为 1，否则用 Jaro-Winkler
func libretroScore(q, c string) float64 {
	if q == c {
		return 1
	}
	// 词数不同时（如 Tetris vs Tetris 2、Contra vs Contra Force）不做模糊匹配，
	// 避免把不同游戏配到同一张封面
	if len(strings.Fields(q)) != len(strings.Fields(c)) {
		return 0
	}
	return jaroWinkler(q, c)
}

// jaroWinkler 计算 Jaro-Winkler 相似度（0~1），用于名称模糊匹配
func jaroWinkler(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	j := jaro(ra, rb)
	if j == 0 {
		return 0
	}
	l := 0
	for l < 4 && l < len(ra) && l < len(rb) && ra[l] == rb[l] {
		l++
	}
	return j + float64(l)*0.1*(1-j)
}

// jaro 计算 Jaro 相似度（0~1）
func jaro(a, b []rune) float64 {
	la, lb := len(a), len(b)
	if la == 0 && lb == 0 {
		return 1
	}
	if la == 0 || lb == 0 {
		return 0
	}
	win := la
	if lb > win {
		win = lb
	}
	win = win/2 - 1
	if win < 0 {
		win = 0
	}
	ma := make([]bool, la)
	mb := make([]bool, lb)
	m := 0
	for i := 0; i < la; i++ {
		lo := i - win
		if lo < 0 {
			lo = 0
		}
		hi := i + win + 1
		if hi > lb {
			hi = lb
		}
		for k := lo; k < hi; k++ {
			if mb[k] || a[i] != b[k] {
				continue
			}
			ma[i], mb[k] = true, true
			m++
			break
		}
	}
	if m == 0 {
		return 0
	}
	t := 0
	k := 0
	for i := 0; i < la; i++ {
		if !ma[i] {
			continue
		}
		for !mb[k] {
			k++
		}
		if a[i] != b[k] {
			t++
		}
		k++
	}
	mf := float64(m)
	return (mf/float64(la) + mf/float64(lb) + (mf-float64(t)/2)/mf) / 3
}