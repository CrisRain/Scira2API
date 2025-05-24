package constants

import "time"

// API 相关常量
const (
	DefaultBaseURL    = "https://scira.crisrain.workers.dev/"
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
