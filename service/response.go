package service

import (
	"bufio"
	"encoding/json"
	"net/http"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/constants"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

// handleRegularResponse 处理常规响应（非流式）
func (h *ChatHandler) handleRegularResponse(c *gin.Context, resp *resty.Response, model string) {
	c.Header("Content-Type", constants.ContentTypeJSON)
	c.Header("Access-Control-Allow-Origin", "*")

	ctx := c.Request.Context()
	scanner := bufio.NewScanner(strings.NewReader(resp.String()))

	var content, reasoningContent string
	usage := models.Usage{}
	finishReason := "stop"

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.Info("Client disconnected during response processing")
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		h.processResponseLine(line, &content, &reasoningContent, &usage, &finishReason)
	}

	if err := scanner.Err(); err != nil {
		log.Error("Error scanning response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process response"})
		return
	}

	// 构造OpenAI格式的响应
	responseID := h.generateResponseID()
	choices := models.NewChoice(content, reasoningContent, finishReason)
	oaiResponse := models.NewOaiStreamResponse(responseID, time.Now().Unix(), model, choices)
	oaiResponse.Usage = usage
	oaiResponse.Object = constants.ObjectChatCompletion // 非流式响应使用不同的object类型

	c.JSON(http.StatusOK, oaiResponse)
}

// processResponseLine 处理响应行
func (h *ChatHandler) processResponseLine(line string, content, reasoningContent *string, usage *models.Usage, finishReason *string) {
	switch {
	case strings.HasPrefix(line, "0:"):
		// 内容部分
		*content += processContent(line[2:])

	case strings.HasPrefix(line, "g:"):
		// 推理内容
		*reasoningContent += processContent(line[2:])

	case strings.HasPrefix(line, "e:"):
		// 完成信息
		h.processFinishData(line[2:], finishReason)

	case strings.HasPrefix(line, "d:"):
		// 用量信息
		h.processUsageData(line[2:], usage)
	}
}

// processFinishData 处理完成数据
func (h *ChatHandler) processFinishData(data string, finishReason *string) {
	var finishData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &finishData); err != nil {
		log.Warn("Failed to parse finish data: %v", err)
		return
	}

	if reason, ok := finishData["finishReason"].(string); ok {
		*finishReason = reason
	}
}

// processUsageData 处理用量数据
func (h *ChatHandler) processUsageData(data string, usage *models.Usage) {
	var usageData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &usageData); err != nil {
		log.Warn("Failed to parse usage data: %v", err)
		return
	}

	if u, ok := usageData["usage"].(map[string]interface{}); ok {
		if pt, ok := u["promptTokens"].(float64); ok {
			usage.PromptTokens = int(pt)
		}
		if ct, ok := u["completionTokens"].(float64); ok {
			usage.CompletionTokens = int(ct)
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}
