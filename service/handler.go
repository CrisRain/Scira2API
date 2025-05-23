package service

import (
	"scira2api/config"
	"scira2api/models"
	"scira2api/pkg/constants"
	"scira2api/pkg/manager"

	"github.com/go-resty/resty/v2"
)

// ChatHandler 聊天处理器结构体
type ChatHandler struct {
	config          *config.Config
	client          *resty.Client
	userManager     *manager.UserManager
	chatIdGenerator *manager.ChatIdGenerator
	streamUsage     *models.Usage // 用于存储流式响应的tokens统计信息
	
	// 存储自己计算的token数据，用于校正统计结果
	calculatedInputTokens  int
	calculatedOutputTokens int
	calculatedTotalTokens  int
}

// NewChatHandler 创建新的聊天处理器实例
func NewChatHandler(cfg *config.Config) *ChatHandler {
	// 创建统一的resty HTTP客户端
	client := createHTTPClient(cfg)

	// 创建管理器组件
	userManager := manager.NewUserManager(cfg.Auth.UserIds)
	chatIdGenerator := manager.NewChatIdGenerator(constants.ChatGroup)

	return &ChatHandler{
		config:          cfg,
		client:          client,
		userManager:     userManager,
		chatIdGenerator: chatIdGenerator,
	}
}

// createHTTPClient 创建HTTP客户端
func createHTTPClient(cfg *config.Config) *resty.Client {
	client := resty.New().
		SetTimeout(cfg.Client.Timeout).
		SetBaseURL(cfg.Client.BaseURL).
		SetHeader("Content-Type", constants.ContentTypeJSON).
		SetHeader("Accept", constants.AcceptAll).
		SetHeader("Origin", cfg.Client.BaseURL).
		SetHeader("User-Agent", constants.UserAgent)

	// 设置代理（如果有）
	if cfg.Client.HttpProxy != "" {
		client.SetProxy(cfg.Client.HttpProxy)
	}

	return client
}

// getUserId 获取用户ID（轮询方式）
func (h *ChatHandler) getUserId() string {
	return h.userManager.GetNextUserId()
}

// getChatId 生成聊天ID
func (h *ChatHandler) getChatId() string {
	return h.chatIdGenerator.GenerateId()
}

// GetConfig 获取配置
func (h *ChatHandler) GetConfig() *config.Config {
	return h.config
}

// GetClient 获取HTTP客户端
func (h *ChatHandler) GetClient() *resty.Client {
	return h.client
}

// resetTokenCalculation 重置token计算数据
func (h *ChatHandler) resetTokenCalculation() {
	h.calculatedInputTokens = 0
	h.calculatedOutputTokens = 0
	h.calculatedTotalTokens = 0
}

// calculateInputTokens 计算请求的提示tokens
func (h *ChatHandler) calculateInputTokens(request models.OpenAIChatCompletionsRequest) {
	// 将消息转换为可计算格式
	messagesInterface := make([]interface{}, len(request.Messages))
	for i, msg := range request.Messages {
		msgMap := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		messagesInterface[i] = msgMap
	}
	
	// 计算提示tokens
	h.calculatedInputTokens = calculateMessageTokens(messagesInterface)
}

// updateOutputTokens 更新完成tokens计算
func (h *ChatHandler) updateOutputTokens(content string) {
	tokens := countTokens(content)
	h.calculatedOutputTokens += tokens
	h.calculatedTotalTokens = h.calculatedInputTokens + h.calculatedOutputTokens
}

// getCalculatedUsage 获取计算得到的usage数据
func (h *ChatHandler) getCalculatedUsage() models.Usage {
	return models.Usage{
		PromptTokens:     h.calculatedInputTokens,
		CompletionTokens: h.calculatedOutputTokens,
		TotalTokens:      h.calculatedTotalTokens,
		PromptTokensDetails: models.PromptTokensDetails{
			CachedTokens: 0,
			AudioTokens:  0,
		},
		CompletionTokensDetails: models.CompletionTokensDetails{
			ReasoningTokens:         0,
			AudioTokens:             0,
			AcceptedPredictionTokens: 0,
			RejectedPredictionTokens: 0,
		},
	}
}
