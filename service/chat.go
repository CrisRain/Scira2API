package service

import (
	"bufio"
	"context"
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

// chatRequestResult 聊天请求结果结构体
type chatRequestResult struct {
	Resp   *resty.Response
	ChatId string
	UserId string
	Err    error
}

// ChatCompletionsHandler 处理聊天完成请求
func (h *ChatHandler) ChatCompletionsHandler(c *gin.Context) {
	// 应用请求限制
	if h.rateLimiter != nil {
		ctx := c.Request.Context()
		if err := h.rateLimiter.Wait(ctx); err != nil {
			log.Warn("请求限制器拒绝请求: %v", err)
			apiErr := errors.NewTooManyRequestsError("请求过于频繁，请稍后重试", err)
			c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
			return
		}
	}

	var request models.OpenAIChatCompletionsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Error("bind json error: %s", err)
		apiErr := errors.NewInvalidRequestError("Failed to parse request JSON", err)
		c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
		return
	}

	if err := h.chatParamCheck(request); err != nil {
		log.Error("chat param check error: %s", err)
		apiErr := errors.NewInvalidRequestError(err.Error(), err)
		c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
		return
	}
	
	// 非流式请求，尝试从缓存获取响应
	if !request.Stream && h.responseCache != nil && h.responseCache.IsEnabled() {
		cachedResponse, found := h.responseCache.GetResponseCache(request)
		if found {
			log.Info("从缓存返回聊天完成响应")
			c.JSON(http.StatusOK, cachedResponse)
			return
		}
	}
	
	// 重置token统计
	h.resetTokenCalculation()
	
	// 计算输入tokens
	h.calculateInputTokens(request)

	if request.Stream {
		if err := h.doChatRequestAsync(c, request); err != nil {
			log.Error("async request failed: %s", err)
			if !c.Writer.Written() { // 只有在还没开始写响应时才返回错误
				apiErr := errors.NewInternalServerError("Stream processing failed", err)
				c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
			}
		}
	} else {
		h.handleSyncRequest(c, request)
	}
}

// handleSyncRequest 处理同步请求
func (h *ChatHandler) handleSyncRequest(c *gin.Context, request models.OpenAIChatCompletionsRequest) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.config.Client.Timeout)
	defer cancel()

	resultChan := h.doChatRequestRegular(ctx, request)

	select {
	case result := <-resultChan:
		resp, chatId, userId, err := result.Resp, result.ChatId, result.UserId, result.Err
		if err != nil {
			log.Error("request failed after retries: %s. UserId: %s, ChatId: %s", err, userId, chatId)
			apiErr := errors.NewServiceUnavailableError("Chat service temporarily unavailable", err)
			c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
			return
		}
		h.handleRegularResponse(c, resp, request.Model, request)

	case <-ctx.Done():
		log.Error("request timeout")
		apiErr := errors.NewInternalServerError("Request timeout", ctx.Err())
		c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
	}
}

// doChatRequestRegular 执行常规聊天请求（非流式）
func (h *ChatHandler) doChatRequestRegular(ctx context.Context, request models.OpenAIChatCompletionsRequest) <-chan chatRequestResult {
	resultChan := make(chan chatRequestResult, constants.ChannelBufferSize)

	go func() {
		defer close(resultChan)

		result := h.executeRequestWithRetry(ctx, request)

		select {
		case resultChan <- result:
		case <-ctx.Done():
			log.Warn("Context cancelled before sending result")
		}
	}()

	return resultChan
}

// executeRequestWithRetry 执行带重试的请求
func (h *ChatHandler) executeRequestWithRetry(ctx context.Context, request models.OpenAIChatCompletionsRequest) chatRequestResult {
	attempts := h.config.Client.Retry
	if attempts <= 0 {
		attempts = constants.DefaultRetryCount
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
			return chatRequestResult{Err: ctx.Err()}
		default:
		}

		chatId := h.getChatId()
		userId := h.getUserId()
		log.Info("Attempt %d/%d: Request use userId: %s, generate chatId: %s", i+1, attempts, userId, chatId)

		resp, err := h.executeRequest(ctx, request, chatId, userId)
		if err == nil {
			log.Info("Attempt %d/%d successful. UserId: %s, ChatId: %s", i+1, attempts, userId, chatId)
			return chatRequestResult{Resp: resp, ChatId: chatId, UserId: userId}
		}

		lastErr = err
		log.Error("Attempt %d/%d failed. UserId: %s, ChatId: %s, Error: %s", i+1, attempts, userId, chatId, err)

		if i < attempts-1 {
			select {
			case <-time.After(constants.RetryDelay):
			case <-ctx.Done():
				return chatRequestResult{Err: ctx.Err()}
			}
		}
	}

	log.Error("All %d attempts failed for chat request. Last error: %s", attempts, lastErr)
	return chatRequestResult{Err: fmt.Errorf("all retry attempts failed: %w", lastErr)}
}

// handleRegularResponse 重写处理常规响应
func (h *ChatHandler) handleRegularResponse(c *gin.Context, resp *resty.Response, model string, request models.OpenAIChatCompletionsRequest) {
	// 调用原始的处理方法处理基本响应
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

		processLineData(line, &content, &reasoningContent, &usage, &finishReason)
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
	
	// 创建响应对象
	responseID := h.generateResponseID()
	
	// 创建OpenAI格式的响应
	openAIResp := &models.OpenAIChatCompletionsResponse{
		ID:      responseID,
		Object:  constants.ObjectChatCompletion,
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ResponseChoice{
			{
				BaseChoice: models.BaseChoice{
					Index:        0,
					FinishReason: finishReason,
				},
				Message: models.ResponseMessage{
					Role:             constants.RoleAssistant,
					Content:          content,
					ReasoningContent: reasoningContent,
				},
			},
		},
		Usage: correctedUsage,
	}
	
	// 缓存响应
	if h.responseCache != nil && h.responseCache.IsEnabled() {
		h.responseCache.SetResponseCache(request, openAIResp)
		log.Debug("已缓存聊天完成响应")
	}
	
	// 返回响应
	c.JSON(http.StatusOK, openAIResp)
}

// executeRequest 执行单次请求
func (h *ChatHandler) executeRequest(ctx context.Context, request models.OpenAIChatCompletionsRequest, chatId, userId string) (*resty.Response, error) {
	sciraRequest := request.ToSciraChatCompletionsRequest(request.Model, chatId, userId)

	resp, err := h.client.R().
		SetContext(ctx).
		SetHeader("Referer", h.config.Client.BaseURL).
		SetBody(sciraRequest).
		Post(constants.APISearchEndpoint)

	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode(), resp.String())
	}

	return resp, nil
}
