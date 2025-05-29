package config

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"scira2api/log"
	"scira2api/pkg/constants"
	"scira2api/pkg/errors"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config 应用配置结构
type Config struct {
	Server          ServerConfig    `json:"server"`
	Auth            AuthConfig      `json:"auth"`
	Client          ClientConfig    `json:"client"`
	Chat            ChatConfig      `json:"chat"`
	Cache           CacheConfig     `json:"cache"`
	ConnPool        ConnPoolConfig  `json:"conn_pool"`
	RateLimit       RateLimitConfig `json:"rate_limit"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port         string        `json:"port"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"` // 新增 IdleTimeout
}

// AuthConfig 认证配置
type AuthConfig struct {
	ApiKey  string   `json:"api_key"`
	UserIds []string `json:"user_ids"`
}

// ClientConfig 客户端配置
type ClientConfig struct {
	HttpProxy       string        `json:"http_proxy"`
	Socks5Proxy     string        `json:"socks5_proxy"`
	DynamicProxy    bool          `json:"dynamic_proxy"`
	ProxyRefreshMin time.Duration `json:"proxy_refresh_min"`
	Timeout         time.Duration `json:"timeout"`
	Retry           int           `json:"retry"`
	BaseURL         string        `json:"base_url"`
}


// ChatConfig 聊天配置
type ChatConfig struct {
	Delete bool `json:"delete"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled         bool          `json:"enabled"`
	ModelCacheTTL   time.Duration `json:"model_cache_ttl"`
	ResponseCacheTTL time.Duration `json:"response_cache_ttl"`
	CleanupInterval time.Duration `json:"cleanup_interval"`
}

// ConnPoolConfig 连接池配置
type ConnPoolConfig struct {
	Enabled             bool          `json:"enabled"`
	MaxIdleConns        int           `json:"max_idle_conns"`
	MaxConnsPerHost     int           `json:"max_conns_per_host"`
	MaxIdleConnsPerHost int           `json:"max_idle_conns_per_host"`
	IdleConnTimeout     time.Duration `json:"idle_conn_timeout"`
}

// RateLimitConfig 速率限制配置
type RateLimitConfig struct {
	Enabled     bool    `json:"enabled"`
	RequestsPerSecond float64 `json:"requests_per_second"`
	Burst       int     `json:"burst"`
}

// NewConfig 创建新的配置实例
func NewConfig() (*Config, error) {
	// 加载环境变量文件
	if err := godotenv.Load(); err != nil {
		log.Warn("Failed to load .env file: %v", err)
	}

	config := &Config{}

	// 加载各种配置
	configLoaders := []struct {
		name   string
		loader func() error
	}{
		{"server", config.loadServerConfig},
		{"auth", config.loadAuthConfig},
		{"client", config.loadClientConfig},
		{"chat", config.loadChatConfig},
		{"cache", config.loadCacheConfig},
		{"conn_pool", config.loadConnPoolConfig},
		{"rate_limit", config.loadRateLimitConfig},
	}

	for _, cl := range configLoaders {
		if err := cl.loader(); err != nil {
			return nil, fmt.Errorf("failed to load %s config: %w", cl.name, err)
		}
	}

	// 验证配置
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", errors.ErrConfigValidation, err)
	}

	return config, nil
}

// loadServerConfig 加载服务器配置
func (c *Config) loadServerConfig() error {
	c.Server.Port = getEnvWithDefault("PORT", constants.DefaultPort)
	c.Server.ReadTimeout = time.Duration(getEnvAsInt("READ_TIMEOUT", int(constants.DefaultReadTimeout.Seconds()))) * time.Second
	c.Server.WriteTimeout = time.Duration(getEnvAsInt("WRITE_TIMEOUT", int(constants.DefaultWriteTimeout.Seconds()))) * time.Second
	// 默认 IdleTimeout 为 5 分钟 (300 秒)，如果环境变量未设置
	c.Server.IdleTimeout = time.Duration(getEnvAsInt("IDLE_TIMEOUT", int(constants.DefaultIdleTimeout.Seconds()))) * time.Second
	return nil
}

// loadAuthConfig 加载认证配置
func (c *Config) loadAuthConfig() error {
	c.Auth.ApiKey = os.Getenv("APIKEY")

	userIdsEnv := os.Getenv("USERIDS")
	if userIdsEnv != "" {
		userIds := strings.Split(userIdsEnv, ",")
		// 清理用户ID，移除空白字符
		var cleanUserIds []string
		for _, id := range userIds {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				cleanUserIds = append(cleanUserIds, trimmed)
			}
		}
		c.Auth.UserIds = cleanUserIds
	} else {
		// 如果没有设置USERIDS，使用默认用户ID
		c.Auth.UserIds = []string{constants.DefaultUserId}
	}
	
	return nil
}

// loadClientConfig 加载客户端配置
func (c *Config) loadClientConfig() error {
	// 加载HTTP代理
	c.Client.HttpProxy = getProxy()
	
	// 加载SOCKS5代理
	c.Client.Socks5Proxy = getEnvWithDefault("SOCKS5_PROXY", "")
	
	// 加载动态代理配置
	dynamicProxyStr := getEnvWithDefault("DYNAMIC_PROXY", "false")
	dynamicProxy, err := strconv.ParseBool(dynamicProxyStr)
	if err != nil {
		return fmt.Errorf("DYNAMIC_PROXY must be true or false, got: %s", dynamicProxyStr)
	}
	c.Client.DynamicProxy = dynamicProxy
	
	// 加载代理刷新间隔
	proxyRefreshStr := getEnvWithDefault("PROXY_REFRESH_MIN", "30m")
	proxyRefresh, err := time.ParseDuration(proxyRefreshStr)
	if err != nil {
		return fmt.Errorf("invalid PROXY_REFRESH_MIN: %s, error: %v", proxyRefreshStr, err)
	}
	c.Client.ProxyRefreshMin = proxyRefresh
	
	// 加载其他客户端配置
	c.Client.Timeout = time.Duration(getEnvAsInt("CLIENT_TIMEOUT", int(constants.DefaultClientTimeout.Seconds()))) * time.Second
	c.Client.BaseURL = getEnvWithDefault("BASE_URL", constants.DefaultBaseURL)

	retry := getEnvAsInt("RETRY", constants.DefaultRetryCount)
	c.Client.Retry = int(math.Max(float64(retry), 1))

	return nil
}


// loadChatConfig 加载聊天配置
func (c *Config) loadChatConfig() error {
	chatDeleteStr := getEnvWithDefault("CHAT_DELETE", "false")
	chatDelete, err := strconv.ParseBool(chatDeleteStr)
	if err != nil {
		return fmt.Errorf("CHAT_DELETE must be true or false, got: %s", chatDeleteStr)
	}
	c.Chat.Delete = chatDelete
	return nil
}

// loadCacheConfig 加载缓存配置
func (c *Config) loadCacheConfig() error {
	// 是否启用缓存
	cacheEnabledStr := getEnvWithDefault(constants.EnvCacheEnabled, "true")
	cacheEnabled, err := strconv.ParseBool(cacheEnabledStr)
	if err != nil {
		return fmt.Errorf("%s must be true or false, got: %s", constants.EnvCacheEnabled, cacheEnabledStr)
	}
	c.Cache.Enabled = cacheEnabled
	
	// 模型缓存TTL
	modelCacheTTLStr := getEnvWithDefault(constants.EnvModelCacheTTL, "")
	if modelCacheTTLStr != "" {
		modelCacheTTL, err := time.ParseDuration(modelCacheTTLStr)
		if err != nil {
			return fmt.Errorf("invalid %s: %s, error: %v", constants.EnvModelCacheTTL, modelCacheTTLStr, err)
		}
		c.Cache.ModelCacheTTL = modelCacheTTL
	} else {
		c.Cache.ModelCacheTTL = constants.DefaultModelCacheTTL
	}
	
	// 响应缓存TTL
	respCacheTTLStr := getEnvWithDefault(constants.EnvRespCacheTTL, "")
	if respCacheTTLStr != "" {
		respCacheTTL, err := time.ParseDuration(respCacheTTLStr)
		if err != nil {
			return fmt.Errorf("invalid %s: %s, error: %v", constants.EnvRespCacheTTL, respCacheTTLStr, err)
		}
		c.Cache.ResponseCacheTTL = respCacheTTL
	} else {
		c.Cache.ResponseCacheTTL = constants.DefaultResponseCacheTTL
	}
	
	// 清理间隔
	cleanupIntervalStr := getEnvWithDefault(constants.EnvCleanupInterval, "")
	if cleanupIntervalStr != "" {
		cleanupInterval, err := time.ParseDuration(cleanupIntervalStr)
		if err != nil {
			return fmt.Errorf("invalid %s: %s, error: %v", constants.EnvCleanupInterval, cleanupIntervalStr, err)
		}
		c.Cache.CleanupInterval = cleanupInterval
	} else {
		c.Cache.CleanupInterval = constants.DefaultCleanupInterval
	}
	
	return nil
}

// loadConnPoolConfig 加载连接池配置
func (c *Config) loadConnPoolConfig() error {
	// 是否启用连接池
	connPoolEnabledStr := getEnvWithDefault("CONN_POOL_ENABLED", "true")
	connPoolEnabled, err := strconv.ParseBool(connPoolEnabledStr)
	if err != nil {
		return fmt.Errorf("CONN_POOL_ENABLED must be true or false, got: %s", connPoolEnabledStr)
	}
	c.ConnPool.Enabled = connPoolEnabled
	
	// 最大空闲连接数
	c.ConnPool.MaxIdleConns = getEnvAsInt("MAX_IDLE_CONNS", 1000)
	
	// 每个主机的最大连接数
	c.ConnPool.MaxConnsPerHost = getEnvAsInt("MAX_CONNS_PER_HOST", runtime.NumCPU()*2)
	
	// 每个主机的最大空闲连接数
	c.ConnPool.MaxIdleConnsPerHost = getEnvAsInt("MAX_IDLE_CONNS_PER_HOST", runtime.NumCPU())
	
	// 空闲连接超时
	idleConnTimeoutStr := getEnvWithDefault("IDLE_CONN_TIMEOUT", "90s")
	idleConnTimeout, err := time.ParseDuration(idleConnTimeoutStr)
	if err != nil {
		return fmt.Errorf("invalid IDLE_CONN_TIMEOUT: %s, error: %v", idleConnTimeoutStr, err)
	}
	c.ConnPool.IdleConnTimeout = idleConnTimeout
	
	return nil
}

// loadRateLimitConfig 加载速率限制配置
func (c *Config) loadRateLimitConfig() error {
	// 是否启用速率限制
	rateLimitEnabledStr := getEnvWithDefault("RATE_LIMIT_ENABLED", "true")
	rateLimitEnabled, err := strconv.ParseBool(rateLimitEnabledStr)
	if err != nil {
		return fmt.Errorf("RATE_LIMIT_ENABLED must be true or false, got: %s", rateLimitEnabledStr)
	}
	c.RateLimit.Enabled = rateLimitEnabled
	
	// 每秒请求数
	requestsPerSecondStr := getEnvWithDefault("REQUESTS_PER_SECOND", "1")
	requestsPerSecond, err := strconv.ParseFloat(requestsPerSecondStr, 64)
	if err != nil {
		return fmt.Errorf("invalid REQUESTS_PER_SECOND: %s, error: %v", requestsPerSecondStr, err)
	}
	c.RateLimit.RequestsPerSecond = requestsPerSecond
	
	// 突发请求数
	c.RateLimit.Burst = getEnvAsInt("BURST", 10)
	
	return nil
}

// validate 验证配置
func (c *Config) validate() error {
	// 验证端口
	if port, err := strconv.Atoi(c.Server.Port); err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port: %s", c.Server.Port)
	}

	// 验证重试次数
	if c.Client.Retry < 1 {
		return fmt.Errorf("retry count must be at least 1")
	}

	return nil
}

// getEnvWithDefault 获取环境变量，如果不存在则返回默认值
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt 获取环境变量并转换为整数
func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Warn("Invalid integer value for %s: %s, using default: %d", key, valueStr, defaultValue)
		return defaultValue
	}

	return value
}

// getProxy 获取代理设置
func getProxy() string {
	if proxy := os.Getenv("HTTP_PROXY"); proxy != "" {
		return proxy
	}
	return os.Getenv("http_proxy")
}

// 兼容性方法
func (c *Config) Port() string {
	return c.Server.Port
}

func (c *Config) ApiKey() string {
	return c.Auth.ApiKey
}

func (c *Config) UserIds() []string {
	return c.Auth.UserIds
}

func (c *Config) HttpProxy() string {
	return c.Client.HttpProxy
}

func (c *Config) Socks5Proxy() string {
	return c.Client.Socks5Proxy
}

func (c *Config) DynamicProxy() bool {
	return c.Client.DynamicProxy
}

func (c *Config) ProxyRefreshMin() time.Duration {
	return c.Client.ProxyRefreshMin
}

func (c *Config) Models() []string {
	// 从ModelMapping中获取所有内部模型名称
	internalModels := make(map[string]bool)
	for _, internalName := range ModelMapping {
		internalModels[internalName] = true
	}
	
	// 转换为切片
	models := make([]string, 0, len(internalModels))
	for model := range internalModels {
		models = append(models, model)
	}
	
	return models
}

func (c *Config) Retry() int {
	return c.Client.Retry
}

func (c *Config) ChatDelete() bool {
	return c.Chat.Delete
}

// GetModelMapping 返回模型映射。
// 此函数允许其他包安全地访问 ModelMapping，
// 而无需直接访问包级变量，从而保持封装性。
func (c *Config) GetModelMapping() map[string]string {
	// 返回 ModelMapping 的副本以防止外部修改
	// 或者直接返回 ModelMapping 如果不担心外部修改。
	// 为了简单和性能，这里直接返回。
	// 如果需要更强的封装，可以考虑返回一个副本。
	return ModelMapping
}
