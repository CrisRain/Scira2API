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

	// 启动心跳机制
	h.startHeartbeat(ctx, c.Writer, flusher)

	// 执行流式请求
	return h.executeStreamRequest(ctx, c, request, flusher)
}

// setSSEHeaders 设置服务器发送事件的响应头
func (h *ChatHandler) setSSEHeaders(c *gin.Context) {
	c.Header("Content-Type", constants.SSEContentType)
	c.Header("Cache-Control", constants.SSECacheControl)
	c.Header("Connection", constants.SSEConnection)
	c.Header("Access-Control-Allow-Origin", "*")
}

// startHeartbeat 启动心跳机制
func (h *ChatHandler) startHeartbeat(ctx context.Context, writer gin.ResponseWriter, flusher http.Flusher) {
	go func() {
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
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.RawBody().Close()

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("HTTP error: %d", resp.StatusCode())
	}

	// 处理响应流
	return h.processResponseStream(ctx, c, resp, request, flusher)
}

// processResponseStream 处理响应流数据
func (h *ChatHandler) processResponseStream(ctx context.Context, c *gin.Context, resp *resty.Response, request models.OpenAIChatCompletionsRequest, flusher http.Flusher) error {
	// 重置统计数据
	h.streamUsage = nil
	
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
			continue // 继续处理下一行，不中断整个流
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	// 发送结束消息
	return h.sendFinalMessage(c.Writer, flusher, responseID, created, request.Model)
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
			Index:        0,
			Delta:        initialDelta,
			FinishReason: "",
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
				Index:        0,
				Delta:        delta,
				FinishReason: "",
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

		// 立即刷新，确保实时传输
		flusher.Flush()
	} else if strings.HasPrefix(line, "d:") {
		// 处理用量数据
		usage := &models.Usage{}
		h.processUsageData(line[2:], usage)
		h.streamUsage = usage // 保存用量数据供后续使用
	}

	return nil
}

// sendFinalMessage 发送结束消息
func (h *ChatHandler) sendFinalMessage(writer gin.ResponseWriter, flusher http.Flusher, responseID string, created int64, model string) error {
	// 发送带有完成原因的最终消息
	finalChoice := []models.Choice{
		{
			Index:        0,
			Delta:        models.Delta{},
			FinishReason: "stop",
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
	if _, err := fmt.Fprint(writer, "data: [DONE]\n\n"); err != nil {
		return fmt.Errorf("error writing [DONE] to stream: %w", err)
	}

	log.Info("Stream completed. Final message and [DONE] sent to client.")
	flusher.Flush()
	return nil
}
