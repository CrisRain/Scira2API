package service

import (
	"bufio"
	"net/http"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/constants"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

// processRegularResponse 处理常规响应（非流式）- 仅供内部使用，新的实现在chat.go中
func (h *ChatHandler) processRegularResponse(c *gin.Context, resp *resty.Response, model string) {
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
	
	// 使用我们自己的方法计算输出tokens
	h.updateOutputTokens(content)
	if len(reasoningContent) > 0 {
		h.updateOutputTokens(reasoningContent)
	}
	
	// 获取我们计算的tokens统计
	calculatedUsage := h.getCalculatedUsage()
	
	// 将我们计算的tokens与服务器返回的进行对比和校正
	correctedUsage := h.correctUsage(usage, calculatedUsage)
	
	// 记录原始和校正后的统计数据
	log.Info("Token统计对比 - 服务器: 输入=%d, 输出=%d, 总计=%d | 计算值: 输入=%d, 输出=%d, 总计=%d",
		usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens,
		calculatedUsage.PromptTokens, calculatedUsage.CompletionTokens, calculatedUsage.TotalTokens)
	
	// 构造OpenAI格式的响应
	responseID := h.generateResponseID()
	choices := models.NewChoice(content, reasoningContent, finishReason)
	oaiResponse := models.NewOaiStreamResponse(responseID, time.Now().Unix(), model, choices)
	oaiResponse.Usage = correctedUsage
	oaiResponse.Object = constants.ObjectChatCompletion // 非流式响应使用不同的object类型

	c.JSON(http.StatusOK, oaiResponse)
}

// correctUsage 校正用量统计数据
func (h *ChatHandler) correctUsage(serverUsage, calculatedUsage models.Usage) models.Usage {
	// 创建校正后的用量数据
	correctedUsage := serverUsage
	
	// 提示tokens校正：如果计算值与服务器返回值相差超过20%，使用计算值
	if serverUsage.PromptTokens > 0 && calculatedUsage.PromptTokens > 0 {
		diff := float64(serverUsage.PromptTokens - calculatedUsage.PromptTokens) / float64(calculatedUsage.PromptTokens)
		if diff > 0.2 || diff < -0.2 {
			log.Warn("提示tokens统计偏差超过20%%，使用计算值：服务器=%d, 计算值=%d",
				serverUsage.PromptTokens, calculatedUsage.PromptTokens)
			correctedUsage.PromptTokens = calculatedUsage.PromptTokens
		}
	} else if serverUsage.PromptTokens == 0 && calculatedUsage.PromptTokens > 0 {
		// 如果服务器没有返回提示tokens，使用计算值
		correctedUsage.PromptTokens = calculatedUsage.PromptTokens
	}
	
	// 完成tokens校正：类似逻辑
	if serverUsage.CompletionTokens > 0 && calculatedUsage.CompletionTokens > 0 {
		diff := float64(serverUsage.CompletionTokens - calculatedUsage.CompletionTokens) / float64(calculatedUsage.CompletionTokens)
		if diff > 0.2 || diff < -0.2 {
			log.Warn("完成tokens统计偏差超过20%%，使用计算值：服务器=%d, 计算值=%d",
				serverUsage.CompletionTokens, calculatedUsage.CompletionTokens)
			correctedUsage.CompletionTokens = calculatedUsage.CompletionTokens
		}
	} else if serverUsage.CompletionTokens == 0 && calculatedUsage.CompletionTokens > 0 {
		correctedUsage.CompletionTokens = calculatedUsage.CompletionTokens
	}
	
	// 重新计算总tokens
	correctedUsage.TotalTokens = correctedUsage.PromptTokens + correctedUsage.CompletionTokens
	
	return correctedUsage
}

// processResponseLine 处理响应行
func (h *ChatHandler) processResponseLine(line string, content, reasoningContent *string, usage *models.Usage, finishReason *string) {
processLineData(line, content, reasoningContent, usage, finishReason)
}
