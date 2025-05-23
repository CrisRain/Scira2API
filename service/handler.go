package service

import (
	"scira2api/config"
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
