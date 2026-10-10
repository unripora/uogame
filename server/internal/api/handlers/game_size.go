package handlers

import (
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// 游戏文件体积：用于首页「按大小排序」
// 计算时机：导入/上传 ROM 时写入；存量数据在列表接口首次命中时惰性补算并落库。

// ensureGameSize 惰性补算游戏体积（size_bytes 为 0 时）
func ensureGameSize(g *models.Game) {
	if g == nil || g.SizeBytes > 0 {
		return
	}
	size := computeGameSize(g.MountPath)
	if size <= 0 {
		return
	}
	g.SizeBytes = size
	_ = database.DB.Model(&models.Game{}).Where("id = ?", g.ID).
		Update("size_bytes", size).Error
}

// backfillGameSizes 批量补算游戏体积（仅处理 size_bytes 仍为 0 的记录）
// 目的：列表接口只对当前页「缺体积」的记录做磁盘盘点，避免每次请求全表遍历磁盘。
func backfillGameSizes(games []models.Game) {
	type pending struct {
		idx  int
		size int64
	}
	todo := make([]pending, 0)
	for i := range games {
		if games[i].SizeBytes > 0 {
			continue
		}
		size := computeGameSize(games[i].MountPath)
		if size <= 0 {
			continue
		}
		games[i].SizeBytes = size
		todo = append(todo, pending{idx: i, size: size})
	}
	if len(todo) == 0 {
		return
	}
	// 单事务批量落库，减少写放大
	_ = database.DB.Transaction(func(tx *gorm.DB) error {
		for _, p := range todo {
			if err := tx.Model(&models.Game{}).Where("id = ?", games[p.idx].ID).
				Update("size_bytes", p.size).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// computeGameSize 计算挂载路径的占用字节数（文件取自身大小，目录递归求和）
func computeGameSize(mountPath string) int64 {
	p := resolveGamePath(mountPath)
	if p == "" {
		return 0
	}
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.Walk(p, func(_ string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil || fi.IsDir() {
			return nil
		}
		total += fi.Size()
		return nil
	})
	return total
}