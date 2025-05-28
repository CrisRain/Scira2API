package constants

import (
	"math/rand"
	"time"
)

// API 相关常量
const (
	DefaultBaseURL    = "https://scira.ai/"
	APISearchEndpoint = "/api/search"
	ContentTypeJSON   = "application/json"
	UserAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	AcceptAll         = "*/*"
)

// 服务器配置常量
const (
	DefaultPort          = "8080"
	DefaultReadTimeout   = 30 * time.Second
	DefaultWriteTimeout  = 30 * time.Second
	DefaultClientTimeout = 300 * time.Second
	DefaultRetryCount    = 1
)

// 响应相关常量
const (
	ObjectChatCompletion      = "chat.completion"
	ObjectChatCompletionChunk = "chat.completion.chunk"
	RoleAssistant             = "assistant"
	ProviderScira             = "scira"
	ChatGroup                 = "chat"
	DefaultTimeZone           = "Asia/Shanghai"
	DefaultUserId             = "default_user" // 默认用户ID
)

// 流式响应常量
const (
	SSEContentType    = "text/event-stream"
	SSECacheControl   = "no-cache"
	SSEConnection     = "keep-alive"
	HeartbeatInterval = 15 * time.Second
	HeartbeatMessage  = ": heartbeat\n\n"
)

// 默认模型列表
const (
	DefaultModels = "gpt-4.1-mini,claude-3-7-sonnet,grok-3-mini,qwen-qwq"
)

// 缓冲区大小
const (
	InitialBufferSize = 128 * 1024      // 128KB
	MaxBufferSize     = 2 * 1024 * 1024 // 2MB
	ChannelBufferSize = 1
)

// 重试和延迟
const (
	RetryDelay         = 500 * time.Millisecond
	RandomStringLength = 10
)

// 缓存相关常量
const (
	// 默认缓存过期时间
	DefaultModelCacheTTL    = 24 * time.Hour  // 模型列表缓存24小时
	DefaultResponseCacheTTL = 5 * time.Minute // 响应缓存5分钟
	DefaultCleanupInterval  = 10 * time.Minute // 每10分钟清理一次过期项
	
	// 缓存键前缀
	ModelCacheKey     = "models"
	ResponseCachePrefix = "response:"
	
	// 缓存配置环境变量
	EnvCacheEnabled   = "CACHE_ENABLED"
	EnvModelCacheTTL  = "MODEL_CACHE_TTL"
	EnvRespCacheTTL   = "RESPONSE_CACHE_TTL"
	EnvCleanupInterval = "CACHE_CLEANUP_INTERVAL"
)

// UserAgent列表
var UserAgentList = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.159 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.0 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:90.0) Gecko/20100101 Firefox/90.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.164 Safari/537.36 Edg/91.0.864.71",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 14_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (iPad; CPU OS 14_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.131 Safari/537.36 OPR/78.0.4093.147",
}

// GetRandomUserAgent 返回随机User-Agent
func GetRandomUserAgent() string {
	rand.Seed(time.Now().UnixNano())
	return UserAgentList[rand.Intn(len(UserAgentList))]
}
