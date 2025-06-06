package service

import (
	"net/http"
	"runtime/debug"
	"scira2api/log"
	"scira2api/models"

	"github.com/gin-gonic/gin"
)

// ModelGetHandler 处理获取模型列表的请求
func (h *ChatHandler) ModelGetHandler(c *gin.Context) {
	// 增加调试信息 - 记录请求来源
	clientIP := c.ClientIP()
	userAgent := c.Request.UserAgent()
	referer := c.Request.Referer()
	log.Debug("收到models请求: IP=%s, User-Agent=%s, Referer=%s", clientIP, userAgent, referer)
	
	// 记录调用堆栈，帮助确定调用来源
	stack := string(debug.Stack())
	log.Debug("ModelGetHandler调用堆栈: %s", stack)
	
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
	// 使用更新后的 Models() 函数获取模型列表
	availableModels := h.config.Models()
	data := make([]models.OpenAIModelResponse, 0, len(availableModels))

	for _, modelID := range availableModels {
		// modelID 这里是内部名称，需要转换为外部名称以供客户端展示
		externalModelID := GetExternalModelName(h.config, modelID)
		model := models.NewModelResponse(externalModelID)
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
