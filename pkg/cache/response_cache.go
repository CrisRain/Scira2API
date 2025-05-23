package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/constants"
	"sync"
	"time"
)

// ResponseCache 用于缓存API响应
type ResponseCache struct {
	modelCache      *Cache
	responseCache   *Cache
	enabled         bool
	mu              sync.RWMutex
}

// ResponseCacheOptions 缓存选项
type ResponseCacheOptions struct {
	ModelCacheTTL      time.Duration
	ResponseCacheTTL   time.Duration
	CleanupInterval    time.Duration
	Enabled            bool
}

// DefaultResponseCacheOptions 返回默认的缓存选项
func DefaultResponseCacheOptions() ResponseCacheOptions {
	return ResponseCacheOptions{
		ModelCacheTTL:      constants.DefaultModelCacheTTL,
		ResponseCacheTTL:   constants.DefaultResponseCacheTTL,
		CleanupInterval:    constants.DefaultCleanupInterval,
		Enabled:            true, // 默认启用缓存
	}
}

// NewResponseCache 创建一个新的响应缓存
func NewResponseCache(options ResponseCacheOptions) *ResponseCache {
	if options.CleanupInterval <= 0 {
		options.CleanupInterval = time.Minute * 10
	}

	return &ResponseCache{
		modelCache:    NewCache(options.ModelCacheTTL, options.CleanupInterval),
		responseCache: NewCache(options.ResponseCacheTTL, options.CleanupInterval),
		enabled:       options.Enabled,
	}
}

// IsEnabled 检查缓存是否启用
func (rc *ResponseCache) IsEnabled() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.enabled
}

// Enable 启用缓存
func (rc *ResponseCache) Enable() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.enabled = true
	log.Info("Response cache enabled")
}

// Disable 禁用缓存
func (rc *ResponseCache) Disable() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.enabled = false
	log.Info("Response cache disabled")
}

// Clear 清空所有缓存
func (rc *ResponseCache) Clear() {
	rc.modelCache.Clear()
	rc.responseCache.Clear()
	log.Info("Response cache cleared")
}

// SetModelCache 缓存模型列表
func (rc *ResponseCache) SetModelCache(models []models.OpenAIModelResponse) {
	if !rc.IsEnabled() {
		return
	}
	
	rc.modelCache.Set(constants.ModelCacheKey, models, 0)
	log.Debug("Models cached, count: %d", len(models))
}

// GetModelCache 获取缓存的模型列表
func (rc *ResponseCache) GetModelCache() ([]models.OpenAIModelResponse, bool) {
	if !rc.IsEnabled() {
		return nil, false
	}
	
	value, found := rc.modelCache.Get(constants.ModelCacheKey)
	if !found {
		return nil, false
	}
	
	models, ok := value.([]models.OpenAIModelResponse)
	if !ok {
		log.Warn("Invalid type in model cache")
		rc.modelCache.Delete(constants.ModelCacheKey)
		return nil, false
	}
	
	log.Debug("Models retrieved from cache, count: %d", len(models))
	return models, true
}

// generateCacheKey 根据请求生成缓存键
func (rc *ResponseCache) generateCacheKey(request models.OpenAIChatCompletionsRequest) string {
	// 移除可能影响缓存键的不相关字段
	requestCopy := request
	requestCopy.Stream = false
	
	// 将请求序列化为JSON
	data, err := json.Marshal(requestCopy)
	if err != nil {
		log.Error("Failed to marshal request for cache key: %v", err)
		return ""
	}
	
	// 计算请求内容的哈希值作为缓存键
	hash := sha256.Sum256(data)
	return constants.ResponseCachePrefix + hex.EncodeToString(hash[:])
}

// SetResponseCache 缓存请求响应
func (rc *ResponseCache) SetResponseCache(request models.OpenAIChatCompletionsRequest, response *models.OpenAIChatCompletionsResponse) {
	if !rc.IsEnabled() || request.Stream {
		return
	}
	
	key := rc.generateCacheKey(request)
	if key == "" {
		return
	}
	
	rc.responseCache.Set(key, response, 0)
	log.Debug("Response cached for key: %s", key)
}

// GetResponseCache 获取缓存的响应
func (rc *ResponseCache) GetResponseCache(request models.OpenAIChatCompletionsRequest) (*models.OpenAIChatCompletionsResponse, bool) {
	if !rc.IsEnabled() || request.Stream {
		return nil, false
	}
	
	key := rc.generateCacheKey(request)
	if key == "" {
		return nil, false
	}
	
	value, found := rc.responseCache.Get(key)
	if !found {
		return nil, false
	}
	
	response, ok := value.(*models.OpenAIChatCompletionsResponse)
	if !ok {
		log.Warn("Invalid type in response cache")
		rc.responseCache.Delete(key)
		return nil, false
	}
	
	log.Debug("Response retrieved from cache for key: %s", key)
	return response, true
}

// GetMetrics 获取缓存指标
func (rc *ResponseCache) GetMetrics() map[string]interface{} {
	modelMetrics := rc.modelCache.GetMetrics()
	responseMetrics := rc.responseCache.GetMetrics()
	
	return map[string]interface{}{
		"enabled":         rc.IsEnabled(),
		"model_cache":     modelMetrics,
		"response_cache":  responseMetrics,
	}
}