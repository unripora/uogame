package handlers

import (
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// GetHardwareInfo 返回硬件信息（不引入额外依赖，Linux 下读取 /proc，其它平台降级）
func GetHardwareInfo(c *gin.Context) {
	info := gin.H{
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"goVersion": runtime.Version(),
		"numCPU":    runtime.NumCPU(),
	}
	if hn, err := os.Hostname(); err == nil {
		info["hostname"] = hn
	}

	// Linux 下读取 /proc 获取 CPU 型号与内存信息
	if runtime.GOOS == "linux" {
		if model := readCPUModel(); model != "" {
			info["cpuModel"] = model
		}
		if total, avail, ok := readMemInfo(); ok {
			info["totalMem"] = total
			info["availMem"] = avail
		}
	}
	c.JSON(http.StatusOK, info)
}

// readCPUModel 从 /proc/cpuinfo 读取 CPU 型号
func readCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// readMemInfo 从 /proc/meminfo 读取总内存与可用内存（单位：字节）
func readMemInfo() (int64, int64, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	var total, avail int64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kb, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = kb * 1024
		case "MemAvailable:":
			avail = kb * 1024
		}
	}
	return total, avail, true
}

// GetPublicSettings 返回公开设置（无需登录）
func GetPublicSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"registration_enabled": isRegistrationOpen(),
		"site_name":            getSettingValue("site_name", "拾游集"),
		"show_platform_tag":    getSettingValue("show_platform_tag", "true") != "false",
	})
}

// PwaProfile 返回 PWA manifest（供 index.html 的 manifest 链接使用）
func PwaProfile(c *gin.Context) {
	siteName := getSettingValue("site_name", "拾游集")
	c.JSON(http.StatusOK, gin.H{
		"name":             siteName,
		"short_name":       siteName,
		"start_url":        "/",
		"display":          "standalone",
		"background_color": "#000000",
		"theme_color":      "#000000",
		"icons": []gin.H{
			{"src": "/icon_64.png", "sizes": "64x64", "type": "image/png"},
			{"src": "/icon_192.png", "sizes": "192x192", "type": "image/png"},
			{"src": "/icon_256.png", "sizes": "256x256", "type": "image/png"},
		},
	})
}

// getSettingValue 读取设置值，缺省返回默认
func getSettingValue(key, def string) string {
	var s models.Setting
	if err := database.DB.Where("key = ?", key).First(&s).Error; err != nil {
		return def
	}
	return s.Value
}
