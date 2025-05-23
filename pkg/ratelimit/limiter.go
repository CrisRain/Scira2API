package ratelimit

import (
	"context"
	"scira2api/log"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter 请求限制器接口
type RateLimiter interface {
	// Allow 判断是否允许当前请求
	Allow() bool
	
	// Wait 阻塞直到允许请求或上下文取消
	Wait(ctx context.Context) error
	
	// GetMetrics 获取指标
	GetMetrics() map[string]interface{}
}

// TokenBucketLimiter 令牌桶限制器
type TokenBucketLimiter struct {
	rate           float64       // 每秒生成的令牌数
	burst          int           // 桶容量
	tokens         float64       // 当前令牌数
	lastTime       time.Time     // 上次更新时间
	mu             sync.Mutex    // 互斥锁
	enabled        bool          // 是否启用
	requestCount   int64         // 请求计数
	allowedCount   int64         // 允许请求计数
	rejectedCount  int64         // 拒绝请求计数
}

// NewTokenBucketLimiter 创建令牌桶限制器
func NewTokenBucketLimiter(rate float64, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		rate:     rate,
		burst:    burst,
		tokens:   float64(burst),
		lastTime: time.Now(),
		enabled:  true,
	}
}

// Allow 判断是否允许当前请求
func (l *TokenBucketLimiter) Allow() bool {
	atomic.AddInt64(&l.requestCount, 1)
	
	if !l.enabled {
		atomic.AddInt64(&l.allowedCount, 1)
		return true
	}
	
	l.mu.Lock()
	defer l.mu.Unlock()
	
	now := time.Now()
	elapsed := now.Sub(l.lastTime).Seconds()
	l.lastTime = now
	
	// 添加新令牌（最多不超过桶容量）
	l.tokens = min(float64(l.burst), l.tokens+elapsed*l.rate)
	
	if l.tokens < 1 {
		atomic.AddInt64(&l.rejectedCount, 1)
		return false
	}
	
	// 消耗一个令牌
	l.tokens--
	atomic.AddInt64(&l.allowedCount, 1)
	return true
}

// Wait 阻塞直到允许请求或上下文取消
func (l *TokenBucketLimiter) Wait(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if l.Allow() {
				return nil
			}
			// 指数退避，避免频繁检查
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Enable 启用限制器
func (l *TokenBucketLimiter) Enable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = true
	log.Info("Rate limiter enabled: %f requests/second, burst: %d", l.rate, l.burst)
}

// Disable 禁用限制器
func (l *TokenBucketLimiter) Disable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = false
	log.Info("Rate limiter disabled")
}

// IsEnabled 检查限制器是否启用
func (l *TokenBucketLimiter) IsEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// GetMetrics 获取指标
func (l *TokenBucketLimiter) GetMetrics() map[string]interface{} {
	l.mu.Lock()
	availableTokens := l.tokens
	l.mu.Unlock()
	
	requestCount := atomic.LoadInt64(&l.requestCount)
	allowedCount := atomic.LoadInt64(&l.allowedCount)
	rejectedCount := atomic.LoadInt64(&l.rejectedCount)
	
	return map[string]interface{}{
		"enabled":          l.IsEnabled(),
		"rate":             l.rate,
		"burst":            l.burst,
		"available_tokens": availableTokens,
		"request_count":    requestCount,
		"allowed_count":    allowedCount,
		"rejected_count":   rejectedCount,
		"rejection_rate":   calculatePercentage(rejectedCount, requestCount),
	}
}

// min 返回两个浮点数中的较小值
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// calculatePercentage 计算百分比
func calculatePercentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}