package service

import (
	"net/http"
	"scira2api/models"

	"github.com/gin-gonic/gin"
)

// ModelGetHandler 处理获取模型列表的请求
func (h *ChatHandler) ModelGetHandler(c *gin.Context) {
	data := make([]models.OpenAIModelResponse, 0, len(h.config.AvailableModels.Available))

	for _, modelID := range h.config.AvailableModels.Available {
		model := models.NewModelResponse(modelID)
		data = append(data, model)
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}
