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
		// 处理提示tokens (prompt_tokens 或 input_tokens)
		if pt, ok := u["prompt_tokens"].(float64); ok {
			usage.PromptTokens = int(pt)
		} else if it, ok := u["input_tokens"].(float64); ok {
			// 兼容旧字段名
			usage.PromptTokens = int(it)
		}
		
		// 处理提示tokens详情
		if ptd, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
			if ct, ok := ptd["cached_tokens"].(float64); ok {
				usage.PromptTokensDetails.CachedTokens = int(ct)
			}
			if at, ok := ptd["audio_tokens"].(float64); ok {
				usage.PromptTokensDetails.AudioTokens = int(at)
			}
		} else if itd, ok := u["input_tokens_details"].(map[string]interface{}); ok {
			// 兼容旧字段名
			if ct, ok := itd["cached_tokens"].(float64); ok {
				usage.PromptTokensDetails.CachedTokens = int(ct)
			}
		}
		
		// 处理完成tokens (completion_tokens 或 output_tokens)
		if ct, ok := u["completion_tokens"].(float64); ok {
			usage.CompletionTokens = int(ct)
		} else if ot, ok := u["output_tokens"].(float64); ok {
			// 兼容旧字段名
			usage.CompletionTokens = int(ot)
		}
		
		// 处理完成tokens详情
		if ctd, ok := u["completion_tokens_details"].(map[string]interface{}); ok {
			if rt, ok := ctd["reasoning_tokens"].(float64); ok {
				usage.CompletionTokensDetails.ReasoningTokens = int(rt)
			}
			if at, ok := ctd["audio_tokens"].(float64); ok {
				usage.CompletionTokensDetails.AudioTokens = int(at)
			}
			if apt, ok := ctd["accepted_prediction_tokens"].(float64); ok {
				usage.CompletionTokensDetails.AcceptedPredictionTokens = int(apt)
			}
			if rpt, ok := ctd["rejected_prediction_tokens"].(float64); ok {
				usage.CompletionTokensDetails.RejectedPredictionTokens = int(rpt)
			}
		} else if otd, ok := u["output_tokens_details"].(map[string]interface{}); ok {
			// 兼容旧字段名
			if rt, ok := otd["reasoning_tokens"].(float64); ok {
				usage.CompletionTokensDetails.ReasoningTokens = int(rt)
			}
		}
		
		// 处理总tokens
		if tt, ok := u["total_tokens"].(float64); ok {
			usage.TotalTokens = int(tt)
		} else {
			// 如果没有提供总数，则计算
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
	}
}
