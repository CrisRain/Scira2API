package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/constants"
	"scira2api/pkg/errors"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

// doChatRequestAsync 执行异步聊天请求（流式）
func (h *ChatHandler) doChatRequestAsync(c *gin.Context, request models.OpenAIChatCompletionsRequest) error {
	// 设置SSE响应头
	h.setSSEHeaders(c)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		log.Error("Streaming unsupported: ResponseWriter does not implement http.Flusher")
		return errors.ErrStreamingNotSupported
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.config.Client.Timeout)
	defer cancel()

	// 使用WaitGroup来跟踪goroutine
	var wg sync.WaitGroup

	// 启动心跳机制
	h.startHeartbeat(ctx, c.Writer, flusher, &wg)

	// 执行流式请求
	err := h.executeStreamRequest(ctx, c, request, flusher)
	
	// 等待心跳goroutine完成
	wg.Wait()
	
	return err
}

// setSSEHeaders 设置服务器发送事件的响应头
func (h *ChatHandler) setSSEHeaders(c *gin.Context) {
	c.Header("Content-Type", constants.SSEContentType)
	c.Header("Cache-Control", constants.SSECacheControl)
	c.Header("Connection", constants.SSEConnection)
	c.Header("Access-Control-Allow-Origin", "*")
}

// startHeartbeat 启动心跳机制
func (h *ChatHandler) startHeartbeat(ctx context.Context, writer gin.ResponseWriter, flusher http.Flusher, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done() // 确保goroutine结束时通知WaitGroup
		
		ticker := time.NewTicker(constants.HeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if _, err := fmt.Fprint(writer, constants.HeartbeatMessage); err != nil {
					log.Error("Error sending heartbeat: %v", err)
					return
				}
				flusher.Flush()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// executeStreamRequest 执行流式请求
func (h *ChatHandler) executeStreamRequest(ctx context.Context, c *gin.Context, request models.OpenAIChatCompletionsRequest, flusher http.Flusher) error {
	attempts := h.getRetryAttempts()

	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chatId := h.getChatId()
		userId := h.getUserId()
		log.Info("Attempt %d/%d: Request use userId: %s, generate chatId: %s", i+1, attempts, userId, chatId)

		if err := h.processStreamResponse(ctx, c, request, chatId, userId, flusher); err == nil {
			log.Info("Attempt %d/%d successful. UserId: %s, ChatId: %s", i+1, attempts, userId, chatId)
			return nil
		} else {
			log.Error("Attempt %d/%d failed. UserId: %s, ChatId: %s, Error: %s", i+1, attempts, userId, chatId, err)

			if i == attempts-1 {
				log.Error("All %d attempts failed for stream request. Last error: %s", attempts, err)
				return err
			}

			select {
			case <-time.After(constants.RetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("all retry attempts failed")
}

// getRetryAttempts 获取重试次数
func (h *ChatHandler) getRetryAttempts() int {
	attempts := h.config.Client.Retry
	if attempts <= 0 {
		attempts = constants.DefaultRetryCount
	}
	return attempts
}

// processStreamResponse 处理流式响应
func (h *ChatHandler) processStreamResponse(ctx context.Context, c *gin.Context, request models.OpenAIChatCompletionsRequest, chatId, userId string, flusher http.Flusher) error {
	sciraRequest := request.ToSciraChatCompletionsRequest(request.Model, chatId, userId)

	// 发送请求
	resp, err := h.client.R().
		SetContext(ctx).
		SetHeader("Referer", h.config.Client.BaseURL).
		SetBody(sciraRequest).
		SetDoNotParseResponse(true).
		Execute("POST", constants.APISearchEndpoint)

	if err != nil {
		return fmt.Errorf("HTTP request failed: %w, URL: %s, Method: POST", err, constants.APISearchEndpoint)
	}
	
	// 确保响应体被关闭
	if resp != nil && resp.RawBody() != nil {
		defer func() {
			if closeErr := resp.RawBody().Close(); closeErr != nil {
				log.Error("关闭响应体失败: %v", closeErr)
			}
		}()
	} else {
		log.Warn("HTTP请求成功但响应体为空")
		return fmt.Errorf("HTTP请求成功但响应体为空")
	}

	if resp.StatusCode() != http.StatusOK {
		// 获取响应体内容
		responseBody := "无法读取响应体"
		if len(resp.Body()) > 0 {
			// 如果响应体内容过长，只显示前1024个字符
			maxLength := 1024
			if len(resp.Body()) > maxLength {
				responseBody = string(resp.Body()[:maxLength]) + "...(截断)"
			} else {
				responseBody = string(resp.Body())
			}
		}
		
		return fmt.Errorf("HTTP错误: 状态码=%d, 响应体=%s, URL=%s, Method=POST",
			resp.StatusCode(), responseBody, constants.APISearchEndpoint)
	}

	// 处理响应流
	return h.processResponseStream(ctx, c, resp, request, flusher)
}

// processResponseStream 处理响应流数据
func (h *ChatHandler) processResponseStream(ctx context.Context, c *gin.Context, resp *resty.Response, request models.OpenAIChatCompletionsRequest, flusher http.Flusher) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("Panic recovered in processResponseStream: %v", r)
			// Ensure an error is returned from the function
			err = fmt.Errorf("panic occurred: %v", r)

			// Send SSE error message and [DONE]
			errorMsgContent := fmt.Sprintf("Internal Server Error during stream processing. Details: %v", r)
			h.sendPanicErrorSSE(c.Writer, flusher, request.Model, errorMsgContent)
		}
	}()

	// 重置统计数据
	h.mu.Lock()
	h.streamUsage = nil
	h.mu.Unlock()
	
	// 重置我们自己的token计算
	h.resetTokenCalculation()
	
	// 计算输入tokens
	h.calculateInputTokens(request)
	
	scanner := bufio.NewScanner(resp.RawBody())

	// 设置更大的缓冲区
	buf := make([]byte, constants.InitialBufferSize)
	scanner.Buffer(buf, constants.MaxBufferSize)

	// 生成OpenAI流式响应的ID和时间戳
	responseID := h.generateResponseID()
	created := time.Now().Unix()

	// 发送初始消息
	if err := h.sendInitialMessage(c.Writer, flusher, responseID, created, request.Model); err != nil {
		return err
	}

	// 错误计数和阈值
	errCount := 0
	const maxErrors = 5 // 最大允许的连续错误数
	
	// 处理流式数据
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.Info("Client disconnected, stopping stream.")
			return nil
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if err := h.processStreamLine(c.Writer, flusher, line, responseID, created, request.Model); err != nil {
			log.Error("Error processing stream line: %v", err)
			errCount++
			
			// 如果连续错误超过阈值，向客户端发送错误通知并中断处理
			if errCount >= maxErrors {
				errMsg := fmt.Sprintf("Too many errors processing stream (reached threshold of %d). Last error: %v", maxErrors, err)
				log.Error(errMsg)
				
				// 发送错误消息给客户端
				errorResponse := models.OpenAIChatCompletionsStreamResponse{
					ID:      responseID,
					Object:  constants.ObjectChatCompletionChunk,
					Created: created,
					Model:   request.Model,
					Choices: []models.Choice{
						{
							BaseChoice: models.BaseChoice{
								Index:        0,
								FinishReason: "error",
							},
							Delta: models.Delta{
								Content: "\n\n[Stream Error: " + errMsg + "]",
							},
						},
					},
				}
				
				errorJSON, jsonErr := json.Marshal(errorResponse)
				if jsonErr == nil {
					fmt.Fprintf(c.Writer, "data: %s\n\n", errorJSON)
					// 分步发送 [DONE] 信号，避免换行符被错误地插入到字符串中间
					fmt.Fprint(c.Writer, "data: ")
					fmt.Fprint(c.Writer, "[DONE]")
					fmt.Fprint(c.Writer, "\n\n")
					flusher.Flush()
				}
				
				return fmt.Errorf("%s", errMsg)
			}
			
			continue // 继续处理下一行，如果错误计数未超过阈值
		} else {
			// 成功处理一行后重置错误计数
			errCount = 0
		}
	}

	scannerError := scanner.Err()

	// 发送结束消息
	// 始终尝试发送最终消息，以确保 [DONE] 被发送。
	// This is crucial for ensuring the client knows the stream has ended, even if there was a scanner error.
	finalMessageErr := h.sendFinalMessage(c.Writer, flusher, responseID, created, request.Model)

	if scannerError != nil {
		if finalMessageErr != nil {
			// 如果发送最终消息也失败了，记录下来，但优先返回 scannerError。
			log.Error("Error sending final message after scanner error (%v): %v", scannerError, finalMessageErr)
		}
		return fmt.Errorf("scanner error: %w", scannerError)
	}

	return finalMessageErr // 返回发送最终消息的错误（如果有）。
}

// generateResponseID 生成OpenAI格式的响应ID
func (h *ChatHandler) generateResponseID() string {
	return fmt.Sprintf("chatcmpl-%s%s", time.Now().Format("20060102150405"), randString(constants.RandomStringLength))
}

// sendInitialMessage 发送初始消息
func (h *ChatHandler) sendInitialMessage(writer gin.ResponseWriter, flusher http.Flusher, responseID string, created int64, model string) error {
	initialDelta := models.Delta{
		Role:             constants.RoleAssistant,
		Content:          "",
		ReasoningContent: "",
	}

	initialChoice := []models.Choice{
		{
			BaseChoice: models.BaseChoice{
				Index:        0,
				FinishReason: "",
			},
			Delta: initialDelta,
		},
	}

	initialResponse := models.OpenAIChatCompletionsStreamResponse{
		ID:      responseID,
		Object:  constants.ObjectChatCompletionChunk,
		Created: created,
		Model:   model,
		Choices: initialChoice,
	}

	initialJSON, err := json.Marshal(initialResponse)
	if err != nil {
		return fmt.Errorf("error marshaling initial response: %w", err)
	}

	if _, err := fmt.Fprintf(writer, "data: %s\n\n", initialJSON); err != nil {
		return fmt.Errorf("error writing initial response: %w", err)
	}

	flusher.Flush()
	return nil
}

// processStreamLine 处理流式数据行
// 添加变量跟踪上次flush时间
var lastFlushTime time.Time
const minFlushInterval = 100 * time.Millisecond

func (h *ChatHandler) processStreamLine(writer gin.ResponseWriter, flusher http.Flusher, line, responseID string, created int64, model string) error {
	// 处理不同类型的数据并转换为OpenAI流式格式
	if strings.HasPrefix(line, "g:") || strings.HasPrefix(line, "0:") {
		var content string
		var reasoningContent string

		if strings.HasPrefix(line, "g:") {
			reasoningContent = processContent(line[2:])
			// 更新推理内容的token计数
			if reasoningContent != "" {
				h.updateOutputTokens(reasoningContent)
			}
		} else if strings.HasPrefix(line, "0:") {
			content = processContent(line[2:])
			// 更新内容的token计数
			if content != "" {
				h.updateOutputTokens(content)
			}
		}

		// 创建OpenAI格式的流式响应
		delta := models.Delta{
			Content:          content,
			ReasoningContent: reasoningContent,
		}

		choice := []models.Choice{
			{
				BaseChoice: models.BaseChoice{
					Index:        0,
					FinishReason: "",
				},
				Delta: delta,
			},
		}

		response := models.OpenAIChatCompletionsStreamResponse{
			ID:      responseID,
			Object:  constants.ObjectChatCompletionChunk,
			Created: created,
			Model:   model,
			Choices: choice,
		}

		// 转换为JSON
		jsonData, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("error marshaling response: %w", err)
		}

		// 发送给客户端
		if _, err := fmt.Fprintf(writer, "data: %s\n\n", jsonData); err != nil {
			return fmt.Errorf("error writing to stream: %w", err)
		}

		// 控制刷新频率，避免过于频繁的flush
		now := time.Now()
		if now.Sub(lastFlushTime) > minFlushInterval {
			flusher.Flush()
			lastFlushTime = now
		}
	} else if strings.HasPrefix(line, "d:") {
		// 处理用量数据
		usage := &models.Usage{}
		var dummyContent, dummyReasoningContent, dummyFinishReason string
		processLineData(line, &dummyContent, &dummyReasoningContent, usage, &dummyFinishReason)
		h.streamUsage = usage // 保存用量数据供后续使用
	}

	return nil
}

// sendFinalMessage 发送结束消息
func (h *ChatHandler) sendFinalMessage(writer gin.ResponseWriter, flusher http.Flusher, responseID string, created int64, model string) error {
	// 发送带有完成原因的最终消息
	finalChoice := []models.Choice{
		{
			BaseChoice: models.BaseChoice{
				Index:        0,
				FinishReason: "stop",
			},
			Delta: models.Delta{},
		},
	}

	finalResponse := models.OpenAIChatCompletionsStreamResponse{
		ID:      responseID,
		Object:  constants.ObjectChatCompletionChunk,
		Created: created,
		Model:   model,
		Choices: finalChoice,
	}

	// 获取我们计算的token统计数据
	calculatedUsage := h.getCalculatedUsage()
	
	// 服务器返回的统计数据
	var serverUsage models.Usage
	if h.streamUsage != nil {
		serverUsage = *h.streamUsage
	}
	
	// 对比和校正token统计
	correctedUsage := h.correctUsage(serverUsage, calculatedUsage)
	
	// 记录原始和校正后的统计数据
	log.Info("流式Token统计对比 - 服务器: 提示=%d, 完成=%d, 总计=%d | 计算值: 提示=%d, 完成=%d, 总计=%d",
		serverUsage.PromptTokens, serverUsage.CompletionTokens, serverUsage.TotalTokens,
		calculatedUsage.PromptTokens, calculatedUsage.CompletionTokens, calculatedUsage.TotalTokens)
	
	// 添加校正后的tokens统计信息
	finalResponse.Usage = correctedUsage

	finalJSON, err := json.Marshal(finalResponse)
	if err != nil {
		return fmt.Errorf("error marshaling final response: %w", err)
	}

	if _, err := fmt.Fprintf(writer, "data: %s\n\n", finalJSON); err != nil {
		return fmt.Errorf("error writing final response: %w", err)
	}

	// 发送完成标记
	// 分步发送 [DONE] 信号，避免换行符被错误地插入到字符串中间
	if _, err := fmt.Fprint(writer, "data: "); err != nil {
		return fmt.Errorf("error writing [DONE] prefix to stream: %w", err)
	}
	if _, err := fmt.Fprint(writer, "[DONE]"); err != nil {
		return fmt.Errorf("error writing [DONE] marker to stream: %w", err)
	}
	if _, err := fmt.Fprint(writer, "\n\n"); err != nil {
		return fmt.Errorf("error writing [DONE] to stream: %w", err)
	}

	log.Info("Stream completed. Final message and [DONE] sent to client.")
	flusher.Flush()
	return nil
}

// sendPanicErrorSSE sends a standardized SSE error message in case of a panic.
func (h *ChatHandler) sendPanicErrorSSE(writer gin.ResponseWriter, flusher http.Flusher, model string, panicDetails string) {
	log.Info("Attempting to send panic error SSE to client.")

	// Generate a new ID and timestamp for this panic event
	errorID := h.generateResponseID()
	createdTime := time.Now().Unix()

	errorResponse := models.OpenAIChatCompletionsStreamResponse{
		ID:      errorID,
		Object:  constants.ObjectChatCompletionChunk,
		Created: createdTime,
		Model:   model, // Model is passed from the main function context
		Choices: []models.Choice{
			{
				BaseChoice: models.BaseChoice{
					Index:        0,
					FinishReason: "error", // Clearly indicate an error
				},
				Delta: models.Delta{
					// Provide a clear error message in the content
					Content: fmt.Sprintf("\n\n[PANIC: %s]", panicDetails),
				},
			},
		},
	}

	errorJSON, jsonErr := json.Marshal(errorResponse)
	if jsonErr != nil {
		log.Error("Failed to marshal panic SSE error response: %v. Sending plain text fallback.", jsonErr)
		// Fallback to plain text if JSON marshalling fails. Escape quotes in panicDetails for JSON-like structure.
		escapedDetails := strings.ReplaceAll(panicDetails, "\"", "'")
		escapedDetails = strings.ReplaceAll(escapedDetails, "\n", " ") // Newlines can break SSE
		if _, writeErr := fmt.Fprintf(writer, "event: error\ndata: {\"error\": \"Internal Server Error\", \"details\": \"%s\"}\n\n", escapedDetails); writeErr != nil {
			log.Error("Failed to write plain text panic SSE error: %v", writeErr)
		}
	} else {
		if _, writeErr := fmt.Fprintf(writer, "data: %s\n\n", errorJSON); writeErr != nil {
			log.Error("Failed to write JSON panic SSE error: %v", writeErr)
		}
	}

	// 分步发送 [DONE] 信号，避免换行符被错误地插入到字符串中间
	if _, writeErr := fmt.Fprint(writer, "data: "); writeErr != nil {
		log.Error("Failed to write data prefix after panic SSE error: %v", writeErr)
	}
	if _, writeErr := fmt.Fprint(writer, "[DONE]"); writeErr != nil {
		log.Error("Failed to write [DONE] marker after panic SSE error: %v", writeErr)
	}
	if _, writeErr := fmt.Fprint(writer, "\n\n"); writeErr != nil {
		log.Error("Failed to write [DONE] after panic SSE error: %v", writeErr)
	}

	flusher.Flush()
	log.Info("Panic error SSE and [DONE] message sent to client.")
}
