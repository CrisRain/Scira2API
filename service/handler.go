package service

import (
	"errors"
	"fmt"
	"scira2api/config"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/cache"
	"scira2api/pkg/connpool"
	"scira2api/pkg/constants"
	httpClient "scira2api/pkg/http"
	"scira2api/pkg/manager"
	"scira2api/pkg/ratelimit"
	"strings"
	"sync"
	"time"
)

// 优化点: 定义自定义错误类型
// 目的: 提高错误处理的精确性和可读性
// 预期效果: 更明确的错误原因，更易于诊断问题
var (
	ErrInvalidConfig   = errors.New("无效的配置")
	ErrProxySetupFailed = errors.New("代理设置失败")
	ErrResourceCleanup = errors.New("资源清理失败")
)

// ChatHandler 聊天处理器结构体
// 优化点: 结构体字段组织和注释优化
// 目的: 提高代码可读性，便于理解各组件的作用
// 预期效果: 更清晰的代码结构，易于维护
type ChatHandler struct {
	// 基础配置
	config          *config.Config          // 全局配置
	
	// 网络与通信组件
	client          *httpClient.HttpClient  // HTTP客户端
	connPool        *connpool.ConnPool      // 连接池
	
	// 用户与会话管理
	userManager     *manager.UserManager    // 用户管理器
	chatIdGenerator *manager.ChatIdGenerator // 会话ID生成器
	
	// 性能优化组件
	responseCache   *cache.ResponseCache    // 响应缓存
	rateLimiter     ratelimit.RateLimiter   // 请求限制器
	
	// 运行时统计与资源管理
	metrics         *handlerMetrics         // 运行时指标
	shutdown        sync.Once               // 确保只执行一次关闭操作
}

// handlerMetrics 处理器运行时指标
// 优化点: 添加详细的指标收集结构
// 目的: 提供更详细的性能和使用情况统计
// 预期效果: 更好的监控和性能调优
type handlerMetrics struct {
	requestCount        int64         // 总请求数
	cacheHitCount       int64         // 缓存命中次数
	proxyUseCount       int64         // 代理使用次数
	proxyErrors         int64         // 代理错误次数
	lastRequestTime     time.Time     // 最后请求时间
	responseLatencies   []time.Duration // 响应延迟历史
	mu                  sync.RWMutex  // 指标读写锁
}

// newHandlerMetrics 创建新的指标收集器
func newHandlerMetrics() *handlerMetrics {
	return &handlerMetrics{
		responseLatencies: make([]time.Duration, 0, 100),
		lastRequestTime:   time.Now(),
	}
}

// ChatHandlerBuilder 聊天处理器构建器
// 优化点: 使用构建器模式
// 目的: 降低初始化复杂度，提高可读性
// 预期效果: 清晰的初始化流程，降低条件判断嵌套
type ChatHandlerBuilder struct {
	config      *config.Config
	connPool    *connpool.ConnPool
	client      *httpClient.HttpClient
	userManager *manager.UserManager
	chatIdGenerator *manager.ChatIdGenerator
	responseCache *cache.ResponseCache
	rateLimiter  ratelimit.RateLimiter
}

// NewChatHandler 创建新的聊天处理器实例
// 优化点: 使用构建器模式重构初始化流程
// 目的: 提高代码可读性，降低条件判断嵌套
// 预期效果: 更清晰、更易维护的初始化流程
func NewChatHandler(cfg *config.Config) *ChatHandler {
	if cfg == nil {
		log.Error("配置为空，无法创建ChatHandler")
		return nil
	}
	
	// 使用构建器模式初始化
	builder := newChatHandlerBuilder(cfg)
	
	// 按照依赖顺序初始化各组件
	builder = builder.setupConnPool().
		setupHTTPClient().
		setupManagers().
		setupCache().
		setupRateLimiter()
	
	// 构建并返回ChatHandler实例
	return builder.build()
}

// newChatHandlerBuilder 创建聊天处理器构建器
func newChatHandlerBuilder(cfg *config.Config) *ChatHandlerBuilder {
	return &ChatHandlerBuilder{
		config: cfg,
	}
}

// setupConnPool 设置连接池
func (b *ChatHandlerBuilder) setupConnPool() *ChatHandlerBuilder {
	cfg := b.config
	
	// 优化点: 减少嵌套，使用默认值
	// 目的: 提高代码可读性
	// 预期效果: 更简洁的初始化逻辑
	poolOptions := &connpool.ConnPoolOptions{
		MaxIdleConns:        30, // 默认值
		MaxConnsPerHost:     10, // 默认值
		MaxIdleConnsPerHost: 5,  // 默认值
		IdleConnTimeout:     90 * time.Second, // 默认值
		TLSHandshakeTimeout: 10 * time.Second,
		KeepAlive:           30 * time.Second,
	}
	
	// 如果启用了连接池，使用配置的参数
	if cfg.ConnPool.Enabled {
		poolOptions.MaxIdleConns = cfg.ConnPool.MaxIdleConns
		poolOptions.MaxConnsPerHost = cfg.ConnPool.MaxConnsPerHost
		poolOptions.MaxIdleConnsPerHost = cfg.ConnPool.MaxIdleConnsPerHost
		poolOptions.IdleConnTimeout = cfg.ConnPool.IdleConnTimeout
		
		log.Info("连接池已启用: MaxIdleConns=%d, MaxConnsPerHost=%d",
			poolOptions.MaxIdleConns, poolOptions.MaxConnsPerHost)
	} else {
		log.Info("连接池已禁用，使用默认HTTP客户端配置")
	}
	
	b.connPool = connpool.NewConnPool(poolOptions)
	return b
}

// setupHTTPClient 设置HTTP客户端
// 优化点: 分离HTTP客户端创建逻辑
// 目的: 降低函数复杂度，提高可读性
// 预期效果: 更模块化的代码结构
func (b *ChatHandlerBuilder) setupHTTPClient() *ChatHandlerBuilder {
	b.client = createHTTPClient(b.config)
	
	// 如果启用了连接池，记录日志
	if b.config.ConnPool.Enabled {
		log.Info("使用标准库HTTP客户端，连接池由http.Transport内部管理")
	}
	
	return b
}

// setupManagers 设置管理器组件
func (b *ChatHandlerBuilder) setupManagers() *ChatHandlerBuilder {
	b.userManager = manager.NewUserManager()
	b.chatIdGenerator = manager.NewChatIdGenerator(constants.ChatGroup)
	return b
}

// setupCache 设置响应缓存
func (b *ChatHandlerBuilder) setupCache() *ChatHandlerBuilder {
	cfg := b.config
	
	// 创建缓存选项
	cacheOptions := cache.ResponseCacheOptions{
		ModelCacheTTL:    cfg.Cache.ModelCacheTTL,
		ResponseCacheTTL: cfg.Cache.ResponseCacheTTL,
		CleanupInterval:  cfg.Cache.CleanupInterval,
		Enabled:          cfg.Cache.Enabled,
	}
	
	b.responseCache = cache.NewResponseCache(cacheOptions)
	
	// 记录缓存状态
	if cfg.Cache.Enabled {
		log.Info("响应缓存已启用: ModelTTL=%v, ResponseTTL=%v",
			cfg.Cache.ModelCacheTTL, cfg.Cache.ResponseCacheTTL)
	} else {
		log.Info("响应缓存已禁用")
	}
	
	return b
}

// setupRateLimiter 设置请求限制器
func (b *ChatHandlerBuilder) setupRateLimiter() *ChatHandlerBuilder {
	cfg := b.config
	
	// 优化点: 简化条件分支
	// 目的: 提高代码可读性
	// 预期效果: 更简洁的初始化逻辑
	limiter := ratelimit.NewTokenBucketLimiter(1000, 1000) // 默认值
	
	if !cfg.RateLimit.Enabled {
		limiter.Disable()
		log.Info("请求限制器已禁用")
	} else {
		// 覆盖默认值
		limiter = ratelimit.NewTokenBucketLimiter(
			cfg.RateLimit.RequestsPerSecond,
			cfg.RateLimit.Burst)
		log.Info("请求限制器已启用: 速率=%f/秒, 突发=%d",
			cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	}
	
	b.rateLimiter = limiter
	return b
}

// build 构建ChatHandler实例
func (b *ChatHandlerBuilder) build() *ChatHandler {
	return &ChatHandler{
		config:          b.config,
		client:          b.client,
		userManager:     b.userManager,
		chatIdGenerator: b.chatIdGenerator,
		responseCache:   b.responseCache,
		connPool:        b.connPool,
		rateLimiter:     b.rateLimiter,
		metrics:         newHandlerMetrics(), // 初始化指标收集
	}
}

// createHTTPClient 创建HTTP客户端
// 优化点: 简化代理设置逻辑，优化代码结构
// 目的: 提高代码可读性，统一代理处理逻辑
// 预期效果: 更易于维护的代理配置代码
func createHTTPClient(cfg *config.Config) *httpClient.HttpClient {
	// 创建基础客户端
	client := httpClient.NewHttpClient().
		SetTimeout(cfg.Client.Timeout).
		SetBaseURL(cfg.Client.BaseURL).
		SetHeader("Content-Type", constants.ContentTypeJSON).
		SetHeader("Accept", constants.AcceptAll).
		SetHeader("Origin", cfg.Client.BaseURL).
		SetHeader("User-Agent", constants.GetRandomUserAgent()).
		SetRetryCount(cfg.Client.Retry - 1) // SetRetryCount是额外重试次数，所以减1

	// 设置重试等待时间
	client.SetRetryWaitTime(constants.RetryDelay)
	client.SetRetryMaxWaitTime(constants.RetryDelay * 5)

	// 配置代理
	configureClientProxy(client, cfg)

	return client
}

// configureClientProxy 配置客户端代理
// 优化点: 分离代理配置逻辑，统一处理
// 目的: 提高代码模块化，易于维护
// 预期效果: 集中的代理配置逻辑，更清晰的代码结构
func configureClientProxy(client *httpClient.HttpClient, cfg *config.Config) {
	// 代理配置优先级：静态SOCKS5代理 > 静态HTTP代理
	switch {
	case cfg.Client.Socks5Proxy != "":
		configureStaticSocks5Proxy(client, cfg.Client.Socks5Proxy)
	case cfg.Client.HttpProxy != "":
		configureStaticHttpProxy(client, cfg.Client.HttpProxy)
	default:
		log.Info("未配置代理，将使用直接连接")
	}
}

// configureStaticSocks5Proxy 配置静态SOCKS5代理
func configureStaticSocks5Proxy(client *httpClient.HttpClient, proxyAddr string) {
	// 添加协议前缀（如果需要）
	if !strings.HasPrefix(proxyAddr, "socks5://") &&
	   !strings.HasPrefix(proxyAddr, "http://") &&
	   !strings.HasPrefix(proxyAddr, "https://") &&
	   !strings.HasPrefix(proxyAddr, "socks4://") {
		// 默认使用SOCKS5协议
		proxyAddr = "socks5://" + proxyAddr
	}
	
	// 设置代理并记录日志
	_, err := client.SetProxy(proxyAddr)
	if err != nil {
		log.Error("设置静态SOCKS5代理失败: %v", err)
		return
	}
	
	log.Info("使用静态SOCKS5代理: %s", proxyAddr)
}

// configureStaticHttpProxy 配置静态HTTP代理
func configureStaticHttpProxy(client *httpClient.HttpClient, proxyAddr string) {
	// 添加协议前缀（如果需要）
	if !strings.HasPrefix(proxyAddr, "http://") &&
	   !strings.HasPrefix(proxyAddr, "https://") {
		// 默认使用HTTP协议
		proxyAddr = "http://" + proxyAddr
	}
	
	// 设置代理并记录日志
	_, err := client.SetProxy(proxyAddr)
	if err != nil {
		log.Error("设置静态HTTP代理失败: %v", err)
		return
	}
	
	log.Info("使用静态HTTP代理: %s", proxyAddr)
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
func (h *ChatHandler) GetClient() *httpClient.HttpClient {
	return h.client
}

// calculateInputTokens 计算请求的提示tokens
// 优化点: 改进注释和变量命名，增加错误处理
// 目的: 提高代码可读性和可维护性
// 预期效果: 更易于理解和维护的代码
func (h *ChatHandler) calculateInputTokens(request models.OpenAIChatCompletionsRequest, counter *TokenCounter) {
	if counter == nil {
		log.Error("Token计数器为空，无法计算输入tokens")
		return
	}
	
	// 记录开始计算的时间，用于性能分析
	startTime := time.Now()
	
	// 将消息转换为可计算格式
	messagesInterface := make([]interface{}, len(request.Messages))
	for i, msg := range request.Messages {
		// 创建包含消息属性的映射
		msgMap := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		
		// 注意: 现有模型中不支持name和function_call字段
		// 如需扩展，请先更新models.Message结构体定义
		
		messagesInterface[i] = msgMap
	}
	
	// 计算提示tokens并更新计数器
	inputTokens := calculateMessageTokens(messagesInterface)
	counter.SetInputTokens(inputTokens)
	
	// 记录指标
	if h.metrics != nil {
		h.metrics.mu.Lock()
		h.metrics.lastRequestTime = time.Now()
		h.metrics.mu.Unlock()
	}
	
	// 记录计算耗时
	log.Debug("计算输入tokens耗时: %v，tokens数量: %d", time.Since(startTime), inputTokens)
}

// updateOutputTokens 更新完成tokens计算
// 优化点: 增加错误处理和性能指标
// 目的: 提高代码健壮性和可观测性
// 预期效果: 更可靠的token计算和更好的性能监控
func (h *ChatHandler) updateOutputTokens(content string, counter *TokenCounter) {
	if counter == nil {
		log.Error("Token计数器为空，无法更新输出tokens")
		return
	}
	
	// 记录开始计算的时间
	startTime := time.Now()
	
	// 计算内容的token数量
	tokens := countTokens(content)
	counter.AddOutputTokens(tokens)
	
	// 记录计算耗时
	log.Debug("计算输出tokens耗时: %v，tokens数量: %d", time.Since(startTime), tokens)
}

// correctUsage 校正用量统计数据
// 优化点: 提取重复逻辑为函数，改进算法结构
// 目的: 减少代码重复，提高可维护性
// 预期效果: 更简洁、更易维护的代码
func (h *ChatHandler) correctUsage(serverUsage, calculatedUsage models.Usage) models.Usage {
	// 创建校正后的用量数据
	correctedUsage := serverUsage
	
	// 校正提示tokens
	correctedUsage.PromptTokens = h.correctTokenCount(
		"提示tokens",
		serverUsage.PromptTokens,
		calculatedUsage.PromptTokens,
	)
	
	// 校正完成tokens
	correctedUsage.CompletionTokens = h.correctTokenCount(
		"完成tokens",
		serverUsage.CompletionTokens,
		calculatedUsage.CompletionTokens,
	)
	
	// 重新计算总tokens
	correctedUsage.TotalTokens = correctedUsage.PromptTokens + correctedUsage.CompletionTokens
	
	return correctedUsage
}

// correctTokenCount 校正单个token计数
// 优化点: 提取公共逻辑为独立函数
// 目的: 减少代码重复，提高可维护性
// 预期效果: 更简洁的代码结构
func (h *ChatHandler) correctTokenCount(tokenType string, serverCount, calculatedCount int) int {
	// 如果服务器返回值为0但计算值大于0，使用计算值
	if serverCount == 0 && calculatedCount > 0 {
		log.Info("%s: 服务器未返回数据，使用计算值=%d", tokenType, calculatedCount)
		return calculatedCount
	}
	
	// 如果两者都大于0，比较差异
	if serverCount > 0 && calculatedCount > 0 {
		// 计算差异比例
		diff := float64(serverCount - calculatedCount) / float64(calculatedCount)
		
		// 差异超过阈值时，使用计算值
		const thresholdPct = 0.2 // 20%差异阈值
		if diff > thresholdPct || diff < -thresholdPct {
			log.Warn("%s统计偏差超过%.0f%%，使用计算值：服务器=%d, 计算值=%d",
				tokenType, thresholdPct*100, serverCount, calculatedCount)
			return calculatedCount
		}
	}
	
	// 默认使用服务器返回值
	return serverCount
}

// GetCacheMetrics 获取缓存指标
// 优化点: 增加安全检查和详细注释
// 目的: 提高代码健壮性和可读性
// 预期效果: 更可靠的指标收集
func (h *ChatHandler) GetCacheMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"enabled": false,
	}
	
	if h.responseCache != nil {
		cacheMetrics := h.responseCache.GetMetrics()
		// 只有在成功获取到指标时才更新返回值
		if cacheMetrics != nil {
			metrics = cacheMetrics
		}
	}
	
	return metrics
}

// GetConnPoolMetrics 获取连接池指标
// 优化点: 增加安全检查和详细注释
// 目的: 提高代码健壮性和可读性
// 预期效果: 更可靠的指标收集
func (h *ChatHandler) GetConnPoolMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"enabled": false,
	}
	
	if h.connPool != nil {
		poolMetrics := h.connPool.GetMetrics()
		// 只有在成功获取到指标时才更新返回值
		if poolMetrics != nil {
			metrics = poolMetrics
		}
	}
	
	return metrics
}

// GetRateLimiterMetrics 获取限流器指标
// 优化点: 增加安全检查和详细注释
// 目的: 提高代码健壮性和可读性
// 预期效果: 更可靠的指标收集
func (h *ChatHandler) GetRateLimiterMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"enabled": false,
	}
	
	if h.rateLimiter != nil {
		limiterMetrics := h.rateLimiter.GetMetrics()
		// 只有在成功获取到指标时才更新返回值
		if limiterMetrics != nil {
			metrics = limiterMetrics
		}
	}
	
	return metrics
}

// Close 关闭ChatHandler及释放所有资源
// 优化点: 完善资源释放机制，增加错误处理
// 目的: 确保所有资源都能正确释放，防止内存泄漏
// 预期效果: 更可靠的资源清理过程
func (h *ChatHandler) Close() error {
	var errs []error
	
	// 使用sync.Once确保只执行一次关闭操作
	h.shutdown.Do(func() {
		log.Info("开始关闭ChatHandler资源...")
		
		// 关闭连接池
		if h.connPool != nil {
			// 安全释放连接池资源
			if closer, ok := interface{}(h.connPool).(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					errs = append(errs, fmt.Errorf("关闭连接池失败: %w", err))
					log.Error("关闭连接池失败: %v", err)
				} else {
					log.Info("连接池资源已成功释放")
				}
			} else {
				// 如果连接池没有实现Close方法
				log.Warn("连接池未实现Close方法，资源可能未完全释放")
				
				// 无法直接访问底层的http.Client，仅记录日志
				log.Warn("连接池未实现Close方法，可能无法完全释放底层连接资源")
			}
		}
		
		// 关闭HTTP客户端
		if h.client != nil {
			if closer, ok := interface{}(h.client).(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					errs = append(errs, fmt.Errorf("关闭HTTP客户端失败: %w", err))
					log.Error("关闭HTTP客户端失败: %v", err)
				} else {
					log.Info("HTTP客户端资源已成功释放")
				}
			} else {
				// 无法直接访问底层Transport，仅记录日志
				log.Info("HTTP客户端资源标记为释放")
			}
		}
		
		// 关闭响应缓存
		if h.responseCache != nil {
			if closer, ok := interface{}(h.responseCache).(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					errs = append(errs, fmt.Errorf("关闭响应缓存失败: %w", err))
					log.Error("关闭响应缓存失败: %v", err)
				} else {
					log.Info("响应缓存资源已成功释放")
				}
			} else {
				log.Info("响应缓存资源已标记为释放")
			}
		}
		
		// 关闭限流器
		if h.rateLimiter != nil {
			if closer, ok := interface{}(h.rateLimiter).(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					errs = append(errs, fmt.Errorf("关闭请求限制器失败: %w", err))
					log.Error("关闭请求限制器失败: %v", err)
				} else {
					log.Info("请求限制器资源已成功释放")
				}
			} else {
				log.Info("请求限制器资源已标记为释放")
			}
		}
		
		log.Info("ChatHandler资源释放完成")
	})
	
	if len(errs) > 0 {
		return fmt.Errorf("%w: 发生%d个错误: %v", ErrResourceCleanup, len(errs), errs)
	}
	
	return nil
}


// GetMetrics 获取所有指标
// 优化点: 添加统一的指标收集接口
// 目的: 提供完整的运行时状态信息
// 预期效果: 更全面的监控数据
func (h *ChatHandler) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})
	
	// 添加基本指标
	if h.metrics != nil {
		h.metrics.mu.RLock()
		metrics["requestCount"] = h.metrics.requestCount
		metrics["cacheHitCount"] = h.metrics.cacheHitCount
		metrics["proxyUseCount"] = h.metrics.proxyUseCount
		metrics["proxyErrors"] = h.metrics.proxyErrors
		metrics["lastRequestTime"] = h.metrics.lastRequestTime.Format(time.RFC3339)
		h.metrics.mu.RUnlock()
	}
	
	// 添加缓存指标
	cacheMetrics := h.GetCacheMetrics()
	for k, v := range cacheMetrics {
		metrics["cache_"+k] = v
	}
	
	// 添加连接池指标
	connPoolMetrics := h.GetConnPoolMetrics()
	for k, v := range connPoolMetrics {
		metrics["connPool_"+k] = v
	}
	
	// 添加限流器指标
	rateLimiterMetrics := h.GetRateLimiterMetrics()
	for k, v := range rateLimiterMetrics {
		metrics["rateLimit_"+k] = v
	}
	
	return metrics
}
