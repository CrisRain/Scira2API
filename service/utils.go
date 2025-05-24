package service

import (
	"crypto/rand"
	"strings"
	"strconv"
	"unicode/utf8"
	"encoding/json"
	"scira2api/log"
	"scira2api/models"
)

// processContent 处理内容，移除引号并处理转义
func processContent(s string) string {
	// 尝试使用 strconv.Unquote，它能处理标准的 Go 转义。
	// strconv.Unquote 要求字符串是被引号包围的。

	// 情况 1: 字符串 s 已经是双引号包围的。
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if unquoted, err := strconv.Unquote(s); err == nil {
			return unquoted
		}
		// 如果有引号但 Unquote 失败 (例如, "\"abc\\x\""), 则会进入回退逻辑。
		// log.Printf("Debug: strconv.Unquote failed for already quoted string: %s, err: %v", s, err)
	} else {
		// 情况 2: 字符串 s 没有被双引号包围。
		// 尝试添加双引号再 Unquote。
		// 这是为了处理类似 "hello\\nworld" 这样的输入，期望得到 "hello\nworld"。
		quotedS := "\"" + s + "\""
		if unquoted, err := strconv.Unquote(quotedS); err == nil {
			return unquoted
		}
		// 如果添加引号后 Unquote 仍然失败, 则会进入回退逻辑。
		// log.Printf("Debug: strconv.Unquote failed for artificially quoted string: %s (original: %s), err: %v", quotedS, s, err)
	}

	// 回退逻辑: 如果 strconv.Unquote 不适用或失败。
	// 此时的 s 是原始输入字符串。
	processedS := s

	// 1. 移除首尾可能存在的双引号。
	// 使用 TrimPrefix 和 TrimSuffix 更安全，避免索引越界。
	processedS = strings.TrimPrefix(processedS, "\"")
	processedS = strings.TrimSuffix(processedS, "\"")

	// 2. 处理常见的转义字符。
	// 使用 strings.NewReplacer 以正确的顺序处理。
	// \\ 必须首先被替换，以避免错误地处理 \\" 中的 \。
	replacer := strings.NewReplacer(
		"\\\\", "\\", // 处理 \\ -> \
		"\\\"", "\"", // 处理 \" -> "
		"\\n", "\n",   // 处理 \n -> newline
		"\\t", "\t",   // 处理 \t -> tab
		"\\r", "\r",   // 处理 \r -> carriage return
	)
	processedS = replacer.Replace(processedS)

	return processedS
}

// randString 生成安全的随机字符串
func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	
	// 使用crypto/rand生成随机字节
	randomBytes := make([]byte, n)
	_, err := rand.Read(randomBytes)
	if err != nil {
		// 如果随机生成失败，记录错误并返回固定字符串
		log.Error("Failed to generate random string: %v", err)
		return "randomfallback"
	}
	
	// 将随机字节映射到字母表
	for i, randomByte := range randomBytes {
		b[i] = letterBytes[randomByte%byte(len(letterBytes))]
	}
	
	return string(b)
}

// countTokens 计算文本中的token数量(近似值)
// 这是一个优化版计算，使用单次遍历
func countTokens(text string) int {
    words := 0
    punctuation := 0
    cjk := 0
    
    // 单词状态跟踪
    inWord := false
    
    // 单次遍历
    for _, r := range text {
        if strings.ContainsRune(" \t\n\r\f\v", r) { // 空白字符
            if inWord {
                words++
                inWord = false
            }
        } else if strings.ContainsRune(".,;:!?()[]{}-_=+*/\\\"'`~@#$%^&<>|", r) {
            punctuation++
            inWord = false
        } else {
            if utf8.RuneLen(r) > 1 { // 多字节字符（CJK等）
                cjk++
            } else {
                inWord = true
            }
        }
    }
    
    // 确保最后一个单词被计数
    if inWord {
        words++
    }
    
    // 粗略估计：每个英文单词约1.3个token，标点符号1个token，CJK字符约1.5个token
    tokenEstimate := int(float64(words) * 1.3) + punctuation + int(float64(cjk) * 1.5)
    
    // 确保至少返回1个token
    if tokenEstimate < 1 && len(strings.TrimSpace(text)) > 0 {
        tokenEstimate = 1
    }
    
    return tokenEstimate
}

// calculateMessageTokens 计算消息的token数量
func calculateMessageTokens(messages []interface{}) int {
	totalTokens := 0
	for _, msg := range messages {
		if message, ok := msg.(map[string]interface{}); ok {
			// 计算角色名的tokens
			if role, ok := message["role"].(string); ok {
				totalTokens += countTokens(role)
			}
			
			// 计算内容的tokens
			if content, ok := message["content"].(string); ok {
				totalTokens += countTokens(content)
			}
		}
	}
	
	// 添加消息格式的基础tokens(每条消息的元数据)
	// 每条消息额外添加约4个tokens作为格式开销
	totalTokens += len(messages) * 4
	
	// 再加上请求本身的固定tokens(约3个)
	totalTokens += 3
	
	return totalTokens
}

// processLineData 处理响应行数据，统一处理不同前缀的行
func processLineData(line string, content, reasoningContent *string, usage *models.Usage, finishReason *string) {
	switch {
	case strings.HasPrefix(line, "0:"):
		// 内容部分
		*content += processContent(line[2:])

	case strings.HasPrefix(line, "g:"):
		// 推理内容
		processed := processContent(line[2:])
		if *reasoningContent == "" {
			*reasoningContent = processed
		} else {
			*reasoningContent += "\n" + processed
		}

	case strings.HasPrefix(line, "e:"):
		// 完成信息，只更新最新的完成原因
		var finishData map[string]interface{}
		if err := json.Unmarshal([]byte(line[2:]), &finishData); err != nil {
			log.Warn("Failed to parse finish data: %v", err)
			return
		}
		if reason, ok := finishData["finishReason"].(string); ok {
			*finishReason = reason
		}

	case strings.HasPrefix(line, "d:"):
		// 用量信息，使用预定义结构体提高解析效率
		var usageData struct {
			Usage struct {
				PromptTokens     float64 `json:"prompt_tokens"`
				InputTokens      float64 `json:"input_tokens"` // 兼容旧字段名
				CompletionTokens float64 `json:"completion_tokens"`
				OutputTokens     float64 `json:"output_tokens"` // 兼容旧字段名
				TotalTokens      float64 `json:"total_tokens"`
				
				PromptTokensDetails struct {
					CachedTokens float64 `json:"cached_tokens"`
					AudioTokens  float64 `json:"audio_tokens"`
				} `json:"prompt_tokens_details"`
				
				InputTokensDetails struct { // 兼容旧字段名
					CachedTokens float64 `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				
				CompletionTokensDetails struct {
					ReasoningTokens         float64 `json:"reasoning_tokens"`
					AudioTokens             float64 `json:"audio_tokens"`
					AcceptedPredictionTokens float64 `json:"accepted_prediction_tokens"`
					RejectedPredictionTokens float64 `json:"rejected_prediction_tokens"`
				} `json:"completion_tokens_details"`
				
				OutputTokensDetails struct { // 兼容旧字段名
					ReasoningTokens float64 `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
		}
		
		if err := json.Unmarshal([]byte(line[2:]), &usageData); err != nil {
			log.Warn("Failed to parse usage data: %v", err)
			return
		}
		
		// 处理提示tokens (prompt_tokens 或 input_tokens)
		if usageData.Usage.PromptTokens > 0 {
			usage.PromptTokens = int(usageData.Usage.PromptTokens)
		} else if usageData.Usage.InputTokens > 0 {
			// 兼容旧字段名
			usage.PromptTokens = int(usageData.Usage.InputTokens)
		}
		
		// 处理提示tokens详情
		if usageData.Usage.PromptTokensDetails.CachedTokens > 0 {
			usage.PromptTokensDetails.CachedTokens = int(usageData.Usage.PromptTokensDetails.CachedTokens)
		}
		if usageData.Usage.PromptTokensDetails.AudioTokens > 0 {
			usage.PromptTokensDetails.AudioTokens = int(usageData.Usage.PromptTokensDetails.AudioTokens)
		}
		
		// 兼容旧字段名
		if usageData.Usage.InputTokensDetails.CachedTokens > 0 {
			usage.PromptTokensDetails.CachedTokens = int(usageData.Usage.InputTokensDetails.CachedTokens)
		}
		
		// 处理完成tokens (completion_tokens 或 output_tokens)
		if usageData.Usage.CompletionTokens > 0 {
			usage.CompletionTokens = int(usageData.Usage.CompletionTokens)
		} else if usageData.Usage.OutputTokens > 0 {
			// 兼容旧字段名
			usage.CompletionTokens = int(usageData.Usage.OutputTokens)
		}
		
		// 处理完成tokens详情
		if usageData.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			usage.CompletionTokensDetails.ReasoningTokens = int(usageData.Usage.CompletionTokensDetails.ReasoningTokens)
		}
		if usageData.Usage.CompletionTokensDetails.AudioTokens > 0 {
			usage.CompletionTokensDetails.AudioTokens = int(usageData.Usage.CompletionTokensDetails.AudioTokens)
		}
		if usageData.Usage.CompletionTokensDetails.AcceptedPredictionTokens > 0 {
			usage.CompletionTokensDetails.AcceptedPredictionTokens = int(usageData.Usage.CompletionTokensDetails.AcceptedPredictionTokens)
		}
		if usageData.Usage.CompletionTokensDetails.RejectedPredictionTokens > 0 {
			usage.CompletionTokensDetails.RejectedPredictionTokens = int(usageData.Usage.CompletionTokensDetails.RejectedPredictionTokens)
		}
		
		// 兼容旧字段名
		if usageData.Usage.OutputTokensDetails.ReasoningTokens > 0 {
			usage.CompletionTokensDetails.ReasoningTokens = int(usageData.Usage.OutputTokensDetails.ReasoningTokens)
		}
		
		// 处理总tokens
		if usageData.Usage.TotalTokens > 0 {
			usage.TotalTokens = int(usageData.Usage.TotalTokens)
		} else {
			// 如果没有提供总数，则计算
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
	}
}
