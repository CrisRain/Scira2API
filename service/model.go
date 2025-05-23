package service

import (
	"net/http"
	"scira2api/log"
	"scira2api/models"

	"github.com/gin-gonic/gin"
)

// ModelGetHandler 处理获取模型列表的请求
func (h *ChatHandler) ModelGetHandler(c *gin.Context) {
	// 尝试从缓存获取模型列表
	if h.responseCache != nil && h.responseCache.IsEnabled() {
		cachedModels, found := h.responseCache.GetModelCache()
		if found {
			log.Debug("从缓存返回模型列表")
			c.JSON(http.StatusOK, gin.H{
				"object": "list",
				"data":   cachedModels,
			})
			return
		}
	}
	
	// 缓存未命中，生成模型列表
	log.Debug("从配置生成模型列表")
	data := make([]models.OpenAIModelResponse, 0, len(h.config.AvailableModels.Available))

	for _, modelID := range h.config.AvailableModels.Available {
		model := models.NewModelResponse(modelID)
		data = append(data, model)
	}
	
	// 保存到缓存
	if h.responseCache != nil && h.responseCache.IsEnabled() {
		h.responseCache.SetModelCache(data)
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}
