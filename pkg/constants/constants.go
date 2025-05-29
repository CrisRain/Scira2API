package constants

import (
	"fmt"
	"math/rand"
	"strings"
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
	DefaultIdleTimeout   = 300 * time.Second // 默认服务器空闲超时，例如5分钟
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

// GetRandomUserAgent 算法随机生成UserAgent字符串
func GetRandomUserAgent() string {
	rand.Seed(time.Now().UnixNano())
	
	// 操作系统和平台
	osNames := []string{
		"Windows NT 10.0", "Windows NT 6.3", "Windows NT 6.1",
		"Macintosh; Intel Mac OS X 10_15", "Macintosh; Intel Mac OS X 10_14",
		"X11; Linux x86_64", "X11; Ubuntu; Linux x86_64",
		"iPhone; CPU iPhone OS 15", "iPad; CPU OS 15",
	}
	
	// 架构
	architectures := []string{"", "Win64; x64", "Win64; IA64", "x86_64", "i686"}
	
	// WebKit版本
	webkitVersions := []string{"537.36", "601.3.9", "605.1.15", "603.3.8"}
	
	// 浏览器和版本
	browserAndVersions := map[string][]string{
		"Chrome": generateVersions(80, 100, 20),
		"Firefox": generateVersions(80, 95, 15),
		"Safari": generateVersions(605, 615, 10),
		"Edge": generateVersions(80, 95, 15),
		"Opera": generateVersions(75, 85, 10),
	}
	
	// 随机选择浏览器
	browsers := []string{"Chrome", "Firefox", "Safari", "Edge", "Opera"}
	browser := browsers[rand.Intn(len(browsers))]
	versions := browserAndVersions[browser]
	browserVersion := versions[rand.Intn(len(versions))]
	
	// 随机选择操作系统
	os := osNames[rand.Intn(len(osNames))]
	
	// 判断是否需要添加架构信息
	arch := ""
	if !strings.Contains(os, "iPhone") && !strings.Contains(os, "iPad") {
		arch = architectures[rand.Intn(len(architectures))]
	}
	
	// 构建平台部分
	platform := os
	if arch != "" {
		platform = platform + "; " + arch
	}
	
	// 随机选择WebKit版本
	webkitVersion := webkitVersions[rand.Intn(len(webkitVersions))]
	
	// 构建完整的UA字符串，根据不同浏览器调整格式
	var userAgent string
	
	switch browser {
	case "Chrome":
		userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s (KHTML, like Gecko) Chrome/%s Safari/%s",
			platform, webkitVersion, browserVersion, webkitVersion)
	case "Firefox":
		userAgent = fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s",
			platform, browserVersion, browserVersion)
	case "Safari":
		if strings.Contains(os, "Mac") {
			userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s (KHTML, like Gecko) Version/%s Safari/%s",
				platform, browserVersion, "15.0", browserVersion)
		} else {
			userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s (KHTML, like Gecko) Version/%s Safari/%s",
				platform, webkitVersion, "15.0", webkitVersion)
		}
	case "Edge":
		userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s (KHTML, like Gecko) Chrome/%s Safari/%s Edg/%s",
			platform, webkitVersion, browserVersion, webkitVersion, browserVersion)
	case "Opera":
		userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s (KHTML, like Gecko) Chrome/%s Safari/%s OPR/%s",
			platform, webkitVersion, browserVersion, webkitVersion, browserVersion)
	}
	
	return userAgent
}

// generateVersions 生成版本号数组
func generateVersions(min, max int, count int) []string {
	versions := make([]string, count)
	for i := 0; i < count; i++ {
		// 主版本号
		major := min + rand.Intn(max-min+1)
		// 次版本号
		minor := rand.Intn(10)
		// 补丁版本号
		patch := rand.Intn(200)
		
		// 格式化版本号
		versions[i] = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	}
	return versions
}
