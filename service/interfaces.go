package service

import (
	"context"
	"scira2api/models"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

// ChatService 聊天服务接口
type ChatService interface {
	// HTTP处理器
	ChatCompletionsHandler(c *gin.Context)
	ModelGetHandler(c *gin.Context)

	// 内部方法
	chatParamCheck(request models.OpenAIChatCompletionsRequest) error
	handleRegularResponse(c *gin.Context, resp *resty.Response, model string)
}

// UserManager 用户管理接口
type UserManager interface {
	GetNextUserId() string
	GetUserCount() int
	GetAllUserIds() []string
}

// ChatIdGenerator 聊天ID生成器接口
type ChatIdGenerator interface {
	GenerateId() string
}

// HTTPClient HTTP客户端接口
type HTTPClient interface {
	R() *resty.Request
	SetTimeout(timeout interface{}) *resty.Client
	SetBaseURL(url string) *resty.Client
	SetHeader(header, value string) *resty.Client
	SetProxy(proxyURL string) *resty.Client
}

// StreamProcessor 流处理器接口
type StreamProcessor interface {
	ProcessStream(ctx context.Context, c *gin.Context, request models.OpenAIChatCompletionsRequest) error
}

// ResponseProcessor 响应处理器接口
type ResponseProcessor interface {
	ProcessRegularResponse(c *gin.Context, resp *resty.Response, model string)
	ProcessStreamResponse(ctx context.Context, c *gin.Context, resp *resty.Response, request models.OpenAIChatCompletionsRequest) error
}

// Validator 验证器接口
type Validator interface {
	ValidateChatRequest(request models.OpenAIChatCompletionsRequest) error
}
