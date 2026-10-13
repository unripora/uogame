package handlers

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// romExtensions 支持的 ROM 扩展名白名单（按扩展名识别类型，识别不到按 H5 处理）
var romExtensions = map[string]string{
	".nes":  "nes",
	".fds":  "nes",
	".unf":  "nes",
	".gb":   "gb",
	".gbc":  "gb",
	".gba":  "gba",
	".sfc":  "snes",
	".smc":  "snes",
	".md":   "segaMD",
	".gen":  "segaMD",
	".smd":  "segaMD",
	".bin":  "segaMD",
	".32x":  "sega32x",
	".sms":  "segaMS",
	".gg":   "segaGG",
	".pce":  "pce",
	".ngp":  "ngp",
	".ngc":  "ngp",
	".ws":   "ws",
	".wsc":  "ws",
	".vb":   "vb",
	".z64":  "n64",
	".n64":  "n64",
	".v64":  "n64",
	".nds":  "nds",
	".cue":  "psx",
	".iso":  "psx",
	".chd":  "psx",
	".pbp":  "psx",
	".a26":  "atari2600",
	".a52":  "atari5200",
	".a78":  "atari7800",
	".col":  "coleco",
	".zip":  "zip",
	".7z":   "7z",
	".swf":  "flash",
	".html": "h5",
	".htm":  "h5",
}

// maxScanDepth ROM 目录最大递归扫描层数（用户裁定：4 层）
const maxScanDepth = 4

// platformTagLabels 平台码 → 中文平台标签（平台码与 romExtensions 的值一致）
var platformTagLabels = map[string]string{
	"h5":        "HTML5",
	"flash":     "Flash",
	"nes":       "红白机 NES",
	"gb":        "任天堂 GB",
	"gba":       "任天堂 GBA",
	"snes":      "超级任天堂 SFC",
	"n64":       "任天堂 N64",
	"nds":       "任天堂 NDS",
	"segaMD":    "世嘉 MD",
	"sega32x":   "世嘉 32X",
	"segaMS":    "世嘉 MS",
	"segaGG":    "世嘉 GG",
	"pce":       "PC Engine",
	"ngp":       "Neo Geo Pocket",
	"ws":        "万代 WS",
	"vb":        "Virtual Boy",
	"psx":       "索尼 PS1",
	"atari2600": "雅达利 2600",
	"atari5200": "雅达利 5200",
	"atari7800": "雅达利 7800",
	"coleco":    "ColecoVision",
	"zip":       "压缩包",
	"7z":        "压缩包",
}

// platformTagForFile 依据文件名（扩展名）返回平台标签，未识别返回空串
func platformTagForFile(name string) string {
	code := detectTypeByExt(name)
	if code == "" {
		return ""
	}
	return platformTagLabels[code]
}

// platformTagFromFiles 依据一组文件名推断平台标签：
// 优先判定 HTML5（存在 .html/.htm 入口），否则取首个可识别的 ROM 扩展名
func platformTagFromFiles(names []string) string {
	for _, n := range names {
		low := toLower(n)
		if strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm") {
			return "HTML5"
		}
	}
	for _, n := range names {
		if tag := platformTagForFile(n); tag != "" {
			return tag
		}
	}
	return ""
}

// detectTypeByExt 根据扩展名识别平台类型，未识别返回空串（按 H5 处理）
func detectTypeByExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return ""
	}
	if t, ok := romExtensions[toLower(ext)]; ok {
		return t
	}
	return ""
}

// copyDirImpl 递归拷贝目录内容
func copyDirImpl(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirImpl(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFileImpl(s, d); err != nil {
			return err
		}
	}
	return nil
}

// copyFileImpl 拷贝单个文件
func copyFileImpl(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// toLower 简单 ASCII 小写转换
func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
