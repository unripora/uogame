package handlers

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// 导入去重功能
// 判据：内容指纹（zip 内条目名 + 大小 + CRC32 排序拼接 → SHA-256 短哈希）
// 指纹相同 = 同一份资源（同一游戏）；名称不再作为重复判据，允许库内重名游戏。
// 指纹相同但名称不同时，新名称将作为「别名」追加到既有记录。

// zipContentHash 计算 zip 内容指纹（不含 config.json）
func zipContentHash(files []*zip.File) string {
	type entry struct {
		name string
		size uint64
		crc  uint32
	}
	entries := make([]entry, 0, len(files))
	for _, f := range files {
		if f.Name == "config.json" {
			continue
		}
		entries = append(entries, entry{name: f.Name, size: f.UncompressedSize64, crc: f.CRC32})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s|%d|%d\n", e.name, e.size, e.crc)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// zipContentHashWithPrefix 计算指纹时先剥离 strip 前缀，使多游戏包
// （条目名为 "<id>/files/xxx"）与单游戏包（"game_files/xxx"）得到一致的指纹。
// 同时跳过名为 config.json 或 <strip>config.json 的配置条目。
func zipContentHashWithPrefix(files []*zip.File, strip string) string {
	type entry struct {
		name string
		size uint64
		crc  uint32
	}
	// childRoot 为 strip 去掉结尾 "files/" 后的子目录前缀（如 "1/"）。
	// 子游戏的 config.json 位于 childRoot+"config.json"（不在 strip 下），
	// 需一并跳过，否则改名会改变 config.json 大小进而改变指纹，
	// 导致指纹相同、仅名称不同的换皮游戏无法命中别名逻辑。
	childRoot := strings.TrimSuffix(strip, "files/")
	entries := make([]entry, 0, len(files))
	for _, f := range files {
		raw := strings.TrimPrefix(f.Name, "./")
		// 跳过子游戏配置条目（任意层级）
		if raw == "config.json" || raw == childRoot+"config.json" || raw == strip+"config.json" {
			continue
		}
		name := raw
		if strings.HasPrefix(name, strip) {
			name = strings.TrimPrefix(name, strip)
		}
		if name == "config.json" || name == "" {
			continue
		}
		entries = append(entries, entry{name: name, size: f.UncompressedSize64, crc: f.CRC32})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s|%d|%d\n", e.name, e.size, e.crc)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// normalizeName 归一化游戏名称，用于名称判重
// 处理：全角转半角、去空白、小写、去除常见副本/版本后缀与分隔符
func normalizeName(name string) string {
	s := strings.TrimSpace(name)
	s = toDBC(s) // 全角 → 半角
	// 去空白
	s = strings.Join(strings.Fields(s), "")
	s = strings.ToLower(s)
	// 去除常见副本 / 版本后缀（可连续出现）
	suffixes := []string{
		"-测试版", "(测试版)", "（测试版）", "-副本", "(副本)", "（副本）",
		"-copy", "(copy)", "副本", "备份", "bak",
	}
	for {
		trimmed := false
		for _, suf := range suffixes {
			if strings.HasSuffix(s, suf) {
				s = strings.TrimSuffix(s, suf)
				trimmed = true
			}
		}
		// 去除结尾的 (数字)
		if n := len(s); n > 2 && s[n-1] == ')' {
			if i := strings.LastIndexByte(s, '('); i >= 0 {
				if isDigits(s[i+1 : n-1]) {
					s = s[:i]
					trimmed = true
				}
			}
		}
		if !trimmed {
			break
		}
	}
	// 去除所有分隔符，进一步归一
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "")
	s = replacer.Replace(s)
	return s
}

// isDigits 判断字符串是否全为数字
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// toDBC 全角字符转半角（空格、常见标点、字母数字）
func toDBC(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == 0x3000:
			r = ' '
		case r >= 0xFF01 && r <= 0xFF5E:
			r -= 0xFEE0
		}
		out = append(out, r)
	}
	return string(out)
}

// DuplicateCheckResult 重复检测结果
type DuplicateCheckResult struct {
	Duplicate bool         `json:"duplicate"`
	Reason    string       `json:"reason,omitempty"` // fingerprint | name
	Existing  *models.Game `json:"existing,omitempty"`
}

// findDuplicate 在库中查找与给定内容指纹相同的游戏。
// 仅以指纹判重：指纹相同即同一份资源（同一游戏）。
// 名称不再参与判重——指纹不同时即使同名也视为不同游戏（允许重名）。
// 注：name 参数保留用于兼容旧调用点（当前不参与判定）。
func findDuplicate(hash, name string) *DuplicateCheckResult {
	if hash == "" {
		return nil
	}
	var games []models.Game
	if err := database.DB.Find(&games).Error; err != nil {
		return nil
	}
	for i := range games {
		if games[i].ContentHash != "" && games[i].ContentHash == hash {
			return &DuplicateCheckResult{Duplicate: true, Reason: "fingerprint", Existing: &games[i]}
		}
	}
	return nil
}

// parseAliases 解析 Game.Aliases（JSON 数组字符串）为切片；非法或空时返回空切片
func parseAliases(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	// 去空串
	cleaned := make([]string, 0, len(out))
	for _, a := range out {
		if strings.TrimSpace(a) != "" {
			cleaned = append(cleaned, a)
		}
	}
	return cleaned
}

// encodeAliases 将别名切片编码为 JSON 数组字符串
func encodeAliases(list []string) string {
	if len(list) == 0 {
		return ""
	}
	b, err := json.Marshal(list)
	if err != nil {
		return ""
	}
	return string(b)
}

// appendAlias 向既有游戏的别名列表追加一个新名称。
// 规则：与主名相同、或已存在于别名中（归一化去重）则跳过；
// 返回 (新别名列表, 是否发生追加)。
func appendAlias(g *models.Game, newName string) ([]string, bool) {
	name := strings.TrimSpace(newName)
	if name == "" {
		return parseAliases(g.Aliases), false
	}
	list := parseAliases(g.Aliases)
	newKey := normalizeName(name)
	// 与主名相同 → 不追加
	if newKey != "" && normalizeName(g.Name) == newKey {
		return list, false
	}
	// 已存在（归一化）→ 不追加
	for _, a := range list {
		if normalizeName(a) == newKey {
			return list, false
		}
	}
	list = append(list, name)
	return list, true
}

// CheckDuplicate 导入前预检：支持上传 zip（multipart file）或 JSON {name}
// POST /api/games/check-duplicate
func CheckDuplicate(c *gin.Context) {
	var hash, name string

	// 依据 Content-Type 分派解析：json 走 body，其余按 multipart 处理
	if strings.Contains(c.ContentType(), "application/json") {
		var payload struct {
			Name string `json:"name"`
		}
		_ = c.ShouldBindJSON(&payload)
		name = payload.Name
	} else {
		if fileHeader, err := c.FormFile("file"); err == nil {
			tmp, cerr := saveUploadTemp(fileHeader)
			if cerr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
				return
			}
			defer removeTemp(tmp)
			zr, zerr := zip.OpenReader(tmp)
			if zerr == nil {
				defer zr.Close()
				hash = zipContentHash(zr.File)
				for _, f := range zr.File {
					if f.Name == "config.json" {
						if gc, ok := decodeGameConfig(f); ok {
							name = gc.Name
						}
						break
					}
				}
			}
		}
		if name == "" {
			name = c.PostForm("name")
		}
	}

	res := findDuplicate(hash, name)
	if res == nil {
		c.JSON(http.StatusOK, DuplicateCheckResult{Duplicate: false})
		return
	}
	// 指纹命中但名称不同 → 语义为「追加别名」，非严格重复
	if res.Existing != nil && normalizeName(res.Existing.Name) != normalizeName(name) {
		if _, added := appendAlias(res.Existing, name); added {
			c.JSON(http.StatusOK, gin.H{
				"duplicate": true,
				"aliased":   true,
				"reason":    res.Reason,
				"existing":  res.Existing,
			})
			return
		}
	}
	c.JSON(http.StatusOK, res)
}

// DedupeGames 扫描存量库，报告重复分组；merge=true 时合并（保留最早 id，迁移 mount_path 后删除其余）
// POST /api/games/dedupe  body(可选): {merge?:bool}
func DedupeGames(c *gin.Context) {
	var payload struct {
		Merge bool `json:"merge"`
	}
	_ = c.ShouldBindJSON(&payload)

	var games []models.Game
	if err := database.DB.Order("id asc").Find(&games).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch games"})
		return
	}

	// 按归一化名称分组；再按指纹分组
	groups := make(map[string][]models.Game)
	for _, g := range games {
		key := normalizeName(g.Name)
		if key == "" {
			key = fmt.Sprintf("__id_%d", g.ID)
		}
		groups[key] = append(groups[key], g)
	}
	// 指纹分组合并到名称分组（指纹命中且名称不同也视为重复）
	hashFirst := make(map[string]uint)
	for _, g := range games {
		if g.ContentHash == "" {
			continue
		}
		if first, ok := hashFirst[g.ContentHash]; ok {
			_ = first
			// 指纹重复：并入同一组（此处仅报告，保守处理）
		} else {
			hashFirst[g.ContentHash] = g.ID
		}
	}

	type dupGroup struct {
		Key      string       `json:"key"`
		KeepID   uint         `json:"keep_id"`
		KeepName string       `json:"keep_name"`
		Remove   []models.Game `json:"remove"`
	}
	var report []dupGroup
	removed := 0
	for key, gs := range groups {
		if len(gs) < 2 {
			continue
		}
		keep := gs[0]
		dg := dupGroup{Key: key, KeepID: keep.ID, KeepName: keep.Name}
		for _, g := range gs[1:] {
			dg.Remove = append(dg.Remove, g)
			if payload.Merge {
				// 迁移 mount_path：保留记录若为空，则继承被删记录的
				if keep.MountPath == "" && g.MountPath != "" {
					keep.MountPath = g.MountPath
					keep.CoverPath = g.CoverPath
					keep.ContentHash = g.ContentHash
					_ = database.DB.Model(&models.Game{}).Where("id = ?", keep.ID).
						Updates(map[string]interface{}{
							"mount_path":   keep.MountPath,
							"cover_path":   keep.CoverPath,
							"content_hash": keep.ContentHash,
						}).Error
				}
				if err := database.DB.Delete(&models.Game{}, g.ID).Error; err == nil {
					removed++
				}
			}
		}
		report = append(report, dg)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "重复检测完成",
		"groups":       report,
		"group_count":  len(report),
		"merged_count": removed,
		"merge":        payload.Merge,
	})
}
