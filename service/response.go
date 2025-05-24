package service

import (
	"scira2api/log"
	"scira2api/models"
)

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

