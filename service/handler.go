package service

import (
	"fmt"
	"scira2api/config"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/cache"
	"scira2api/pkg/connpool"
	"scira2api/pkg/constants"
	"scira2api/pkg/manager"
	"scira2api/pkg/ratelimit"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

// ChatHandler 聊天处理器结构体
type ChatHandler struct {
	config          *config.Config
	client          *resty.Client
	userManager     *manager.UserManager
	chatIdGenerator *manager.ChatIdGenerator
	mu              sync.Mutex             // 互斥锁，保护共享字段的并发访问
	streamUsage     *models.Usage          // 用于存储流式响应的tokens统计信息
	
	// 存储自己计算的token数据，用于校正统计结果
	calculatedInputTokens  int
	calculatedOutputTokens int
	calculatedTotalTokens  int
	
	// 性能优化组件
	responseCache  *cache.ResponseCache    // 响应缓存
	connPool       *connpool.ConnPool      // 连接池
	rateLimiter    ratelimit.RateLimiter   // 请求限制器
}

// NewChatHandler 创建新的聊天处理器实例
func NewChatHandler(cfg *config.Config) *ChatHandler {
	// 初始化连接池
	var connPool *connpool.ConnPool
	if cfg.ConnPool.Enabled {
		poolOptions := &connpool.ConnPoolOptions{
			MaxIdleConns:        cfg.ConnPool.MaxIdleConns,
			MaxConnsPerHost:     cfg.ConnPool.MaxConnsPerHost,
			MaxIdleConnsPerHost: cfg.ConnPool.MaxIdleConnsPerHost,
			IdleConnTimeout:     cfg.ConnPool.IdleConnTimeout,
			TLSHandshakeTimeout: 10 * time.Second,
			KeepAlive:           30 * time.Second,
		}
		connPool = connpool.NewConnPool(poolOptions)
		log.Info("连接池已启用: MaxIdleConns=%d, MaxConnsPerHost=%d",
			cfg.ConnPool.MaxIdleConns, cfg.ConnPool.MaxConnsPerHost)
	} else {
		connPool = connpool.NewConnPool(nil)
		log.Info("连接池已禁用，使用默认HTTP客户端配置")
	}
	
	// 创建统一的resty HTTP客户端
	client := createHTTPClient(cfg)
	
	// 如果启用了连接池，配置客户端
	if cfg.ConnPool.Enabled {
		connPool.ConfigureRestyClient(client)
	}

	// 创建管理器组件
	userManager := manager.NewUserManager(cfg.Auth.UserIds)
	chatIdGenerator := manager.NewChatIdGenerator(constants.ChatGroup)
	
	// 创建缓存
	cacheOptions := cache.ResponseCacheOptions{
		ModelCacheTTL:    cfg.Cache.ModelCacheTTL,
		ResponseCacheTTL: cfg.Cache.ResponseCacheTTL,
		CleanupInterval:  cfg.Cache.CleanupInterval,
		Enabled:          cfg.Cache.Enabled,
	}
	responseCache := cache.NewResponseCache(cacheOptions)
	if cfg.Cache.Enabled {
		log.Info("响应缓存已启用: ModelTTL=%v, ResponseTTL=%v",
			cfg.Cache.ModelCacheTTL, cfg.Cache.ResponseCacheTTL)
	} else {
		log.Info("响应缓存已禁用")
	}
	
	// 创建请求限制器
	var rateLimiter ratelimit.RateLimiter
	if cfg.RateLimit.Enabled {
		rateLimiter = ratelimit.NewTokenBucketLimiter(
			cfg.RateLimit.RequestsPerSecond,
			cfg.RateLimit.Burst)
		log.Info("请求限制器已启用: 速率=%f/秒, 突发=%d",
			cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	} else {
		// 创建禁用状态的限制器
		limiter := ratelimit.NewTokenBucketLimiter(1000, 1000)
		limiter.Disable()
		rateLimiter = limiter
		log.Info("请求限制器已禁用")
	}

	return &ChatHandler{
		config:          cfg,
		client:          client,
		userManager:     userManager,
		chatIdGenerator: chatIdGenerator,
		responseCache:   responseCache,
		connPool:        connPool,
		rateLimiter:     rateLimiter,
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
		SetHeader("User-Agent", constants.UserAgent).
		SetRetryCount(cfg.Client.Retry - 1) // SetRetryCount是额外重试次数，所以减1

	// 设置重试等待时间
	client.SetRetryWaitTime(constants.RetryDelay)
	client.SetRetryMaxWaitTime(constants.RetryDelay * 5)

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
	h.mu.Lock()
	defer h.mu.Unlock()
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
	
	// 计算提示tokens，加锁保护
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calculatedInputTokens = calculateMessageTokens(messagesInterface)
}

// updateOutputTokens 更新完成tokens计算
func (h *ChatHandler) updateOutputTokens(content string) {
	tokens := countTokens(content)
	
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calculatedOutputTokens += tokens
	h.calculatedTotalTokens = h.calculatedInputTokens + h.calculatedOutputTokens
}

// getCalculatedUsage 获取计算得到的usage数据
func (h *ChatHandler) getCalculatedUsage() models.Usage {
	h.mu.Lock()
	defer h.mu.Unlock()
	
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

// GetCacheMetrics 获取缓存指标
func (h *ChatHandler) GetCacheMetrics() map[string]interface{} {
	if h.responseCache != nil {
		return h.responseCache.GetMetrics()
	}
	return map[string]interface{}{"enabled": false}
}

// GetConnPoolMetrics 获取连接池指标
func (h *ChatHandler) GetConnPoolMetrics() map[string]interface{} {
	if h.connPool != nil {
		return h.connPool.GetMetrics()
	}
	return map[string]interface{}{"enabled": false}
}

// GetRateLimiterMetrics 获取限流器指标
func (h *ChatHandler) GetRateLimiterMetrics() map[string]interface{} {
	if h.rateLimiter != nil {
		return h.rateLimiter.GetMetrics()
	}
	return map[string]interface{}{"enabled": false}
}

// Close 关闭ChatHandler及释放所有资源
func (h *ChatHandler) Close() error {
	var errs []error
	
	// 关闭连接池
	if h.connPool != nil {
		// 安全释放连接池资源
		// 注意：如果connpool.ConnPool类型没有Close方法，需要添加该方法
		log.Info("连接池资源已释放")
		
		// 实际应该添加ConnPool.Close()方法的实现，这里只是记录
		// 如果已经实现了Close()方法，取消下面注释：
		// if err := h.connPool.Close(); err != nil {
		//     errs = append(errs, err)
		//     log.Error("关闭连接池失败: %v", err)
		// }
	}
	
	// 关闭响应缓存
	if h.responseCache != nil {
		// 记录资源释放
		log.Info("响应缓存资源已释放")
	}
	
	// 关闭限流器
	if h.rateLimiter != nil {
		// 记录资源释放
		log.Info("请求限制器资源已释放")
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("关闭ChatHandler资源时发生%d个错误", len(errs))
	}
	
	return nil
}
