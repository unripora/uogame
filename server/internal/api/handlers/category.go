package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// ListCategories 列出全部分类
// 前端约定：返回**裸数组**
func ListCategories(c *gin.Context) {
	var categories []models.Category
	if err := database.DB.Order("sort_order asc, id asc").Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch categories"})
		return
	}
	c.JSON(http.StatusOK, categories)
}

// GetCategoriesStats 返回分类统计信息
// 前端约定：{count:number, last_updated:number}
func GetCategoriesStats(c *gin.Context) {
	var count int64
	database.DB.Model(&models.Category{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{
		"count":        count,
		"last_updated": count,
	})
}

// CreateCategory 新建分类
func CreateCategory(c *gin.Context) {
	var cat models.Category
	if err := c.ShouldBindJSON(&cat); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if err := database.DB.Create(&cat).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create category"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "创建成功", "category": cat})
}

// UpdateCategory 更新分类
func UpdateCategory(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var cat models.Category
	if err := database.DB.First(&cat, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}
	var payload models.Category
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	payload.ID = cat.ID
	if err := database.DB.Save(&payload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update category"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "更新成功", "category": payload})
}

// DeleteCategory 删除分类（同时清理游戏关联，避免孤儿）
func DeleteCategory(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := database.DB.Delete(&models.Category{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete category"})
		return
	}
	// 清理游戏-分类关联
	_ = database.DB.Where("category_id = ?", id).Delete(&models.GameCategory{}).Error
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
