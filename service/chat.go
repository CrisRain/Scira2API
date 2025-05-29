package service

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"scira2api/config"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/constants"
	"scira2api/pkg/errors"
	httpClient "scira2api/pkg/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 优化点: 添加请求结果结构体的详细注释
// 目的: 可读性
// 预期效果: 更清晰地说明结构体的用途和字段含义
// chatRequestResult 聊天请求结果结构体，用于在异步处理中传递HTTP响应结果
type chatRequestResult struct {
	Resp   *httpClient.Response // HTTP响应对象
	ChatId string               // 会话ID
	UserId string               // 用户ID
	Err    error                // 请求过程中的错误
}

// 优化点: 改进主处理函数结构
// 目的: 提高代码可读性和职责分离
// 预期效果: 更清晰的处理流程和错误处理
// ChatCompletionsHandler 处理聊天完成请求
func (h *ChatHandler) ChatCompletionsHandler(c *gin.Context) {
	// 预处理请求
	request, err := h.preprocessChatRequest(c)
	if err != nil {
		// 错误已在预处理函数中处理
		return
	}
	
	// 创建新的token计数器，确保每个请求数据隔离
	tokenCounter := NewTokenCounter()
	
	// 计算输入tokens
	h.calculateInputTokens(request, tokenCounter)

	// 明确分离流式和非流式处理路径
	if request.Stream {
		h.handleStreamRequest(c, request, tokenCounter)
	} else {
		h.handleSyncRequest(c, request, tokenCounter)
	}
}

// 优化点: 抽取请求预处理逻辑为独立函数
// 目的: 提高代码可读性和可维护性
// 预期效果: 主函数更简洁，职责更明确
func (h *ChatHandler) preprocessChatRequest(c *gin.Context) (models.OpenAIChatCompletionsRequest, error) {
	var request models.OpenAIChatCompletionsRequest
	
	// 应用请求限制
	if h.rateLimiter != nil {
		ctx := c.Request.Context()
		if err := h.rateLimiter.Wait(ctx); err != nil {
			log.Warn("请求限制器拒绝请求: %v", err)
			apiErr := errors.NewTooManyRequestsError("请求过于频繁，请稍后重试", err)
			c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
			return request, err
		}
	}

	// 解析请求体
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Error("绑定JSON错误: %s", err)
		apiErr := errors.NewInvalidRequestError("无法解析请求JSON", err)
		c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
		return request, err
	}

	// 参数检查
	if err := h.chatParamCheck(request); err != nil {
		log.Error("聊天参数检查错误: %s", err)
		apiErr := errors.NewInvalidRequestError(err.Error(), err)
		c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
		return request, err
	}
	
	// 非流式请求，尝试从缓存获取响应
	if !request.Stream && h.responseCache != nil && h.responseCache.IsEnabled() {
		cachedResponse, found := h.responseCache.GetResponseCache(request)
		if found {
			log.Info("从缓存返回聊天完成响应")
			c.JSON(http.StatusOK, cachedResponse)
			return request, fmt.Errorf("使用缓存响应")
		}
	}
	
	return request, nil
}

// 优化点: 添加流式请求处理包装函数
// 目的: 统一错误处理和日志记录
// 预期效果: 更一致的错误处理机制
func (h *ChatHandler) handleStreamRequest(c *gin.Context, request models.OpenAIChatCompletionsRequest, counter *TokenCounter) {
	reqID := fmt.Sprintf("req_%s", randString(8))
	log.Info("[%s] 开始处理流式请求", reqID)
	
	if err := h.doChatRequestAsync(c, request, counter); err != nil {
		log.Error("[%s] 异步请求失败: %s", reqID, err)
		if !c.Writer.Written() { // 只有在还没开始写响应时才返回错误
			apiErr := errors.NewInternalServerError("流处理失败", err)
			c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
		}
	}
	
	log.Info("[%s] 流式请求处理完成", reqID)
}

// 优化点: 改进同步请求处理函数
// 目的: 提高错误处理和日志记录的一致性
// 预期效果: 更易于诊断问题和跟踪请求生命周期
// handleSyncRequest 处理同步请求
func (h *ChatHandler) handleSyncRequest(c *gin.Context, request models.OpenAIChatCompletionsRequest, counter *TokenCounter) {
	reqID := fmt.Sprintf("req_%s", randString(8))
	log.Info("[%s] 开始处理同步请求", reqID)
	
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.config.Client.Timeout)
	defer cancel()

	resultChan := h.doChatRequestRegular(ctx, request, counter, reqID)

	select {
	case result := <-resultChan:
		resp, chatId, userId, err := result.Resp, result.ChatId, result.UserId, result.Err
		if err != nil {
			log.Error("[%s] 请求在重试后失败: %s. UserId: %s, ChatId: %s", reqID, err, userId, chatId)
			apiErr := errors.NewServiceUnavailableError("聊天服务暂时不可用", err)
			c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
			return
		}
		log.Info("[%s] 请求成功，开始处理响应", reqID)
		h.handleRegularResponse(c, resp, request.Model, request, counter, reqID)

	case <-ctx.Done():
		log.Error("[%s] 请求超时: %v", reqID, ctx.Err())
		apiErr := errors.NewInternalServerError("请求超时", ctx.Err())
		c.JSON(apiErr.Code, gin.H{"error": apiErr.Message})
	}
	
	log.Info("[%s] 同步请求处理完成", reqID)
}

// 优化点: 改进常规请求处理函数
// 目的: 提高错误跟踪和上下文传递
// 预期效果: 更容易跟踪单个请求的完整生命周期
// doChatRequestRegular 执行常规聊天请求（非流式）
func (h *ChatHandler) doChatRequestRegular(ctx context.Context, request models.OpenAIChatCompletionsRequest, counter *TokenCounter, reqID string) <-chan chatRequestResult {
	resultChan := make(chan chatRequestResult, constants.ChannelBufferSize)

	go func() {
		defer close(resultChan)
		log.Info("[%s] 开始执行带重试的请求", reqID)
		
		result := h.executeRequestWithRetry(ctx, request, reqID)

		select {
		case resultChan <- result:
			log.Debug("[%s] 请求结果已发送到通道", reqID)
		case <-ctx.Done():
			log.Warn("[%s] 上下文在发送结果前取消: %v", reqID, ctx.Err())
		}
	}()

	return resultChan
}

// 优化点: 改进重试逻辑，增加指数退避机制
// 目的: 提高系统在高负载下的稳定性
// 预期效果: 减少对下游服务的压力，提高成功率
// executeRequestWithRetry 执行带重试的请求
func (h *ChatHandler) executeRequestWithRetry(ctx context.Context, request models.OpenAIChatCompletionsRequest, reqID string) chatRequestResult {
	attempts := h.config.Client.Retry
	if attempts <= 0 {
		attempts = constants.DefaultRetryCount
	}

	var lastErr error
	baseDelay := constants.RetryDelay
	
	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
			log.Info("[%s] 上下文取消，终止重试", reqID)
			return chatRequestResult{Err: ctx.Err()}
		default:
		}

		chatId := h.getChatId()
		userId := h.getUserId()
		log.Info("[%s] 尝试 %d/%d: 使用 userId: %s, 生成 chatId: %s", reqID, i+1, attempts, userId, chatId)

		resp, err := h.executeRequest(ctx, request, chatId, userId, reqID)
		if err == nil {
			log.Info("[%s] 尝试 %d/%d 成功. UserId: %s, ChatId: %s", reqID, i+1, attempts, userId, chatId)
			return chatRequestResult{Resp: resp, ChatId: chatId, UserId: userId}
		}

		lastErr = err
		log.Error("[%s] 尝试 %d/%d 失败. UserId: %s, ChatId: %s, 错误: %s", reqID, i+1, attempts, userId, chatId, err)

		if i < attempts-1 {
			// 优化点: 指数退避策略
			// 目的: 在遇到错误时减轻对服务的压力
			// 预期效果: 避免在服务不可用时产生大量重试请求
			retryDelay := time.Duration(float64(baseDelay) * (1.0 + float64(i)*0.5))
			maxDelay := 5 * time.Second
			if retryDelay > maxDelay {
				retryDelay = maxDelay
			}
			
			log.Info("[%s] 等待 %v 后重试", reqID, retryDelay)
			
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return chatRequestResult{Err: ctx.Err()}
			}
		}
	}

	log.Error("[%s] 所有 %d 次尝试均失败. 最后错误: %s", reqID, attempts, lastErr)
	return chatRequestResult{Err: fmt.Errorf("all retry attempts failed: %w", lastErr)}
}

// 优化点: 改进响应处理函数，添加请求ID跟踪，提取处理逻辑
// 目的: 提高代码可读性和可维护性
// 预期效果: 更清晰的响应处理流程，更易于追踪问题
// handleRegularResponse 处理常规响应
func (h *ChatHandler) handleRegularResponse(c *gin.Context, resp *httpClient.Response, model string, request models.OpenAIChatCompletionsRequest, counter *TokenCounter, reqID string) {
	log.Info("[%s] 开始处理常规响应", reqID)
	
	// 确保响应中使用的是外部模型名称
	externalModel := h.getExternalModelName(model, reqID)
	
	// 设置响应头
	h.setResponseHeaders(c)

	// 解析响应内容
	content, reasoningContent, usage, finishReason, err := h.parseResponseBody(c, resp, reqID)
	if err != nil {
		log.Error("[%s] 解析响应失败: %v", reqID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "处理响应失败"})
		return
	}
	
	// 处理token计数
	correctedUsage := h.processTokenCounting(content, reasoningContent, usage, counter, reqID)
	
	// 创建并返回最终响应
	h.createAndSendResponse(c, request, externalModel, content, reasoningContent, finishReason, correctedUsage, reqID)
	
	log.Info("[%s] 常规响应处理完成", reqID)
}

// 优化点: 提取模型名称处理为独立函数
// 目的: 提高代码可读性和可维护性
// 预期效果: 更清晰的代码结构
func (h *ChatHandler) getExternalModelName(model string, reqID string) string {
	if _, exists := config.ModelMapping[model]; exists {
		// 如果传入的是外部模型名，直接使用
		log.Debug("[%s] 使用外部模型名: %s", reqID, model)
		return model
	} else {
		// 如果传入的是内部模型名，尝试转换为外部模型名
		externalName := GetExternalModelName(model)
		log.Debug("[%s] 将内部模型名 %s 转换为外部模型名: %s", reqID, model, externalName)
		return externalName
	}
}

// 优化点: 提取响应头设置为独立函数
// 目的: 提高代码可读性
// 预期效果: 更清晰的代码结构
func (h *ChatHandler) setResponseHeaders(c *gin.Context) {
	c.Header("Content-Type", constants.ContentTypeJSON)
	c.Header("Access-Control-Allow-Origin", "*")
}

// 优化点: 提取响应体解析为独立函数
// 目的: 提高代码可读性和可维护性
// 预期效果: 更清晰的响应处理流程
func (h *ChatHandler) parseResponseBody(c *gin.Context, resp *httpClient.Response, reqID string) (content, reasoningContent string, usage models.Usage, finishReason string, err error) {
	ctx := c.Request.Context()
	bodyBytes := resp.Body()
	bodyString := string(bodyBytes)
	
	log.Debug("[%s] 开始解析响应体，大小: %d 字节", reqID, len(bodyBytes))
	
	scanner := bufio.NewScanner(strings.NewReader(bodyString))
	// 设置较大的缓冲区以处理长行
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	content = ""
	reasoningContent = ""
	usage = models.Usage{}
	finishReason = "stop"

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.Info("[%s] 客户端在响应处理期间断开连接", reqID)
			return "", "", models.Usage{}, "", ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		processLineData(line, &content, &reasoningContent, &usage, &finishReason)
	}

	if err := scanner.Err(); err != nil {
		log.Error("[%s] 扫描响应时出错: %v", reqID, err)
		return "", "", models.Usage{}, "", err
	}
	
	log.Debug("[%s] 响应体解析完成，内容长度: %d, 推理内容长度: %d", reqID, len(content), len(reasoningContent))
	return content, reasoningContent, usage, finishReason, nil
}

// 优化点: 提取token计数处理为独立函数
// 目的: 提高代码可读性和可维护性
// 预期效果: 更清晰的token处理流程
func (h *ChatHandler) processTokenCounting(content, reasoningContent string, usage models.Usage, counter *TokenCounter, reqID string) models.Usage {
	// 使用我们自己的方法计算输出tokens
	h.updateOutputTokens(content, counter)
	if len(reasoningContent) > 0 {
		h.updateOutputTokens(reasoningContent, counter)
	}
	
	// 获取我们计算的tokens统计
	calculatedUsage := counter.GetUsage()
	
	// 将我们计算的tokens与服务器返回的进行对比和校正
	correctedUsage := h.correctUsage(usage, calculatedUsage)
	
	// 记录原始和校正后的统计数据
	log.Info("[%s] Token统计对比 - 服务器: 输入=%d, 输出=%d, 总计=%d | 计算值: 输入=%d, 输出=%d, 总计=%d",
		reqID, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens,
		calculatedUsage.PromptTokens, calculatedUsage.CompletionTokens, calculatedUsage.TotalTokens)
	
	return correctedUsage
}

// 优化点: 提取响应创建和发送为独立函数
// 目的: 提高代码可读性和可维护性
// 预期效果: 更清晰的响应创建流程
func (h *ChatHandler) createAndSendResponse(c *gin.Context, request models.OpenAIChatCompletionsRequest,
	model, content, reasoningContent, finishReason string, usage models.Usage, reqID string) {
	
	// 创建响应对象
	responseID := h.generateResponseID()
	log.Debug("[%s] 生成响应ID: %s", reqID, responseID)
	
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
		Usage: usage,
	}
	
	// 缓存响应
	if h.responseCache != nil && h.responseCache.IsEnabled() {
		h.responseCache.SetResponseCache(request, openAIResp)
		log.Debug("[%s] 已缓存聊天完成响应", reqID)
	}
	
	// 返回响应
	c.JSON(http.StatusOK, openAIResp)
	log.Debug("[%s] 已向客户端发送JSON响应", reqID)
}

// 优化点: 改进请求执行函数，添加请求ID跟踪
// 目的: 提高日志跟踪能力
// 预期效果: 更容易关联请求和日志
// executeRequest 执行单次请求
func (h *ChatHandler) executeRequest(ctx context.Context, request models.OpenAIChatCompletionsRequest, chatId, userId string, reqID string) (*httpClient.Response, error) {
	// 将外部模型名称映射为内部模型名称
	internalModel := MapModelName(request.Model)
	sciraRequest := request.ToSciraChatCompletionsRequest(internalModel, chatId, userId)

	log.Debug("[%s] 发送请求到 %s，模型: %s -> %s", reqID, constants.APISearchEndpoint, request.Model, internalModel)
	
	// 确保使用随机User-Agent
	resp, err := h.client.R().
		SetContext(ctx).
		SetHeader("Referer", h.config.Client.BaseURL).
		SetHeader("User-Agent", constants.GetRandomUserAgent()).
		SetHeader("X-Request-ID", reqID). // 添加请求ID到头部，便于跟踪
		SetBody(sciraRequest).
		Post(constants.APISearchEndpoint)

	if err != nil {
		return nil, fmt.Errorf("HTTP请求失败: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		bodyStr := ""
		if body := resp.Body(); body != nil {
			bodyStr = string(body)
			// 如果响应体过长，进行截断
			maxLen := 500
			if len(bodyStr) > maxLen {
				bodyStr = bodyStr[:maxLen] + "...(截断)"
			}
		}
		return nil, fmt.Errorf("HTTP错误: 状态码=%d, 响应体=%s", resp.StatusCode(), bodyStr)
	}

	log.Debug("[%s] 请求成功, 响应大小: %d 字节", reqID, len(resp.Body()))
	return resp, nil
}
