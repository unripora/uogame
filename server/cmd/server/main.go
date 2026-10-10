package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"uogame-go/internal/api"
	"uogame-go/internal/api/handlers"
	"uogame-go/internal/config"
	"uogame-go/internal/database"
)

// main 程序入口：加载配置 → 初始化数据库 → 启动 HTTP 服务
func main() {
	cfg := config.Load()

	// 确保关键目录存在
	for _, dir := range []string{cfg.DataDir, cfg.UploadDir, cfg.ImagesDir, cfg.LogDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("创建目录失败 %s: %v", dir, err)
		}
	}

	// JWT 密钥：环境变量优先，其次 jwt.secret 文件，最后生成并持久化
	initJWTSecret(cfg)

	// 清理过期日志
	checkAndCleanLog(cfg.LogDir)

	// 初始化数据库
	database.Init(cfg)

	// 启动聊天广播中心
	go handlers.ChatManagerRun()

	// 构建路由并启动服务
	router := api.SetupRouter(cfg)
	addr := "0.0.0.0:" + cfg.Port
	log.Printf("拾游集服务启动，监听 %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// initJWTSecret 初始化 JWT 密钥
func initJWTSecret(cfg *config.Config) {
	if cfg.JWTSecret != "" {
		return
	}
	secretFile := filepath.Join(cfg.DataDir, "jwt.secret")
	if data, err := os.ReadFile(secretFile); err == nil && len(data) > 0 {
		cfg.JWTSecret = string(data)
		return
	}
	// 生成基于时间的简易密钥并持久化
	secret := []byte("lite-game-" + time.Now().Format("20060102150405"))
	_ = os.WriteFile(secretFile, secret, 0o600)
	cfg.JWTSecret = string(secret)
}

// checkAndCleanLog 清理超过 7 天的日志文件
func checkAndCleanLog(logDir string) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(logDir, e.Name()))
		}
	}
}
