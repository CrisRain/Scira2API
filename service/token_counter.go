package service

import (
	"scira2api/models"
	"sync"
)

// TokenCounter 跟踪单个请求的token统计
type TokenCounter struct {
	mu                 sync.Mutex
	inputTokens        int
	outputTokens       int
	totalTokens        int
	streamUsage        *models.Usage
}

// NewTokenCounter 创建新的token计数器
func NewTokenCounter() *TokenCounter {
	return &TokenCounter{}
}

// ResetCalculation 重置token计算数据
func (tc *TokenCounter) ResetCalculation() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.inputTokens = 0
	tc.outputTokens = 0
	tc.totalTokens = 0
}

// SetInputTokens 设置输入tokens数量
func (tc *TokenCounter) SetInputTokens(tokens int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.inputTokens = tokens
	tc.totalTokens = tc.inputTokens + tc.outputTokens
}

// AddOutputTokens 添加输出tokens数量
func (tc *TokenCounter) AddOutputTokens(tokens int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.outputTokens += tokens
	tc.totalTokens = tc.inputTokens + tc.outputTokens
}

// GetUsage 获取当前的usage统计
func (tc *TokenCounter) GetUsage() models.Usage {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	
	return models.Usage{
		PromptTokens:     tc.inputTokens,
		CompletionTokens: tc.outputTokens,
		TotalTokens:      tc.totalTokens,
	}
}

// SetStreamUsage 设置从服务器获取的统计数据
func (tc *TokenCounter) SetStreamUsage(usage *models.Usage) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.streamUsage = usage
}

// GetStreamUsage 获取从服务器获取的统计数据
func (tc *TokenCounter) GetStreamUsage() *models.Usage {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.streamUsage
}