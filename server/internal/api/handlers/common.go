package handlers

import (
	"github.com/gin-gonic/gin"

	"uogame-go/internal/config"
)

// Cfg 全局配置，由 SetupRouter 在启动时注入
var Cfg *config.Config

// Init 注入配置到 handler 包
func Init(cfg *config.Config) {
	Cfg = cfg
}

// GetConfig 返回当前请求使用的配置（当前为全局配置）
func GetConfig(c *gin.Context) *config.Config {
	return Cfg
}

// copyDir 递归拷贝目录，用于游戏导入
func copyDir(src, dst string) error {
	return copyDirImpl(src, dst)
}
