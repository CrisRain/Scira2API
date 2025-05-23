package service

import (
	"context"
	"fmt"
	"net/http"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/constants"
	"scira2api/pkg/errors"
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
		h.handleRegularResponse(c, resp, request.Model)

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
