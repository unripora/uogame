package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Config 应用运行时配置
// 全部来自环境变量（飞牛平台 cmd/main 注入），见还原蓝图 3.3
type Config struct {
	Port          string // PORT，默认 8666
	DataDir       string // DATA_DIR，数据目录
	StorageRoot   string // STORAGE_ROOT，用户共享存储根（游戏文件存放处）
	GamesDir      string // 游戏文件根目录（STORAGE_ROOT/UOGame/games 或 DATA_DIR/games）
	DBPath        string // DB_PATH，SQLite 文件路径
	UploadDir     string // UPLOAD_DIR，上传目录
	ImagesDir     string // IMAGES_DIR，图片目录
	LogDir        string // LOG_DIR，日志目录
	JWTSecret     string // JWT_SECRET，密钥；缺省时从 jwt.secret 文件读取或生成
	AuthCookie    string // AUTH_COOKIE_NAME，认证 Cookie 名
	AdminUsername string // ADMIN_USERNAME，初始管理员用户名
	AdminPassword string // ADMIN_PASSWORD，初始管理员密码
	CORSOrigins   []string // CORS_ORIGINS，允许的跨域来源（逗号分隔；空=不限但需显式回显，见 cors.go）
}

// Load 从环境变量加载配置，补全默认值
func Load() *Config {
	c := &Config{
		Port:          getEnv("PORT", "8666"),
		DataDir:       getEnv("DATA_DIR", defaultDataDir()),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		AuthCookie:    getEnv("AUTH_COOKIE_NAME", "auth_token"),
		AdminUsername: getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "admin123"),
	}

	// 数据库路径：优先 DB_PATH，否则落到数据目录 uogame.sqlite
	c.DBPath = getEnv("DB_PATH", filepath.Join(c.DataDir, "uogame.sqlite"))
	c.UploadDir = getEnv("UPLOAD_DIR", filepath.Join(c.DataDir, "uploads"))
	c.ImagesDir = getEnv("IMAGES_DIR", filepath.Join(c.DataDir, "images"))
	c.LogDir = getEnv("LOG_DIR", filepath.Join(c.DataDir, "logs"))

	// 游戏文件存储根：优先 STORAGE_ROOT（飞牛共享根），否则回退数据目录
	c.StorageRoot = getEnv("STORAGE_ROOT", c.DataDir)

	// 游戏文件目录：默认放在存储根的 UOGame/games 下（若存储根即数据目录，则直接 data/games）
	if c.StorageRoot == c.DataDir {
		c.GamesDir = filepath.Join(c.DataDir, "games")
	} else {
		c.GamesDir = filepath.Join(c.StorageRoot, "UOGame", "games")
	}

	// CORS 白名单：逗号分隔；空表示未配置（中间件按同源放行，不开放跨域）
	c.CORSOrigins = parseCSV(os.Getenv("CORS_ORIGINS"))

	return c
}

// parseCSV 解析逗号分隔列表，去空白、去空项
func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// getEnv 读取环境变量，为空时返回默认值
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// defaultDataDir 缺省数据目录（本地开发/测试使用）
func defaultDataDir() string {
	if v := os.Getenv("TRIM_PKGVAR"); v != "" {
		return v
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "data")
	}
	return "./data"
}
