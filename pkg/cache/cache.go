package cache

import (
	"sync"
	"time"
)

// Item 表示缓存项
type Item struct {
	Value      interface{}
	Expiration int64
	Created    int64
}

// 检查缓存项是否过期
func (item Item) IsExpired() bool {
	if item.Expiration == 0 {
		return false
	}
	return time.Now().UnixNano() > item.Expiration
}

// Cache 表示一个内存缓存
type Cache struct {
	items             map[string]Item
	mu                sync.RWMutex
	defaultExpiration time.Duration
	cleanupInterval   time.Duration
	stopCleanup       chan bool
	metrics           *Metrics
}

// Metrics 表示缓存指标
type Metrics struct {
	Hits      int64
	Misses    int64
	Size      int64
	mu        sync.RWMutex
	startTime time.Time
}

// NewMetrics 创建新的指标实例
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// RecordHit 记录缓存命中
func (m *Metrics) RecordHit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Hits++
}

// RecordMiss 记录缓存未命中
func (m *Metrics) RecordMiss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Misses++
}

// UpdateSize 更新缓存大小
func (m *Metrics) UpdateSize(size int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Size = size
}

// GetStats 获取指标统计
func (m *Metrics) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	uptime := time.Since(m.startTime).Seconds()
	totalRequests := m.Hits + m.Misses
	hitRate := float64(0)
	if totalRequests > 0 {
		hitRate = float64(m.Hits) / float64(totalRequests) * 100
	}
	
	return map[string]interface{}{
		"hits":           m.Hits,
		"misses":         m.Misses,
		"size":           m.Size,
		"hit_rate":       hitRate,
		"uptime_seconds": uptime,
	}
}

// NewCache 创建一个新的缓存实例
// defaultExpiration 是默认的缓存项过期时间
// cleanupInterval 是过期缓存项的清理间隔
// 如果 cleanupInterval <= 0，则不会自动清理过期项
func NewCache(defaultExpiration, cleanupInterval time.Duration) *Cache {
	cache := &Cache{
		items:             make(map[string]Item),
		defaultExpiration: defaultExpiration,
		cleanupInterval:   cleanupInterval,
		stopCleanup:       make(chan bool),
		metrics:           NewMetrics(),
	}

	// 启动定期清理
	if cleanupInterval > 0 {
		go cache.startCleanupTimer()
	}

	return cache
}

// startCleanupTimer 启动清理定时器
func (c *Cache) startCleanupTimer() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-c.stopCleanup:
			return
		}
	}
}

// Set 添加一个带有过期时间的缓存项
func (c *Cache) Set(key string, value interface{}, duration time.Duration) {
	var expiration int64

	if duration == 0 {
		duration = c.defaultExpiration
	}

	if duration > 0 {
		expiration = time.Now().Add(duration).UnixNano()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = Item{
		Value:      value,
		Expiration: expiration,
		Created:    time.Now().UnixNano(),
	}
	
	// 更新缓存大小指标
	c.metrics.UpdateSize(int64(len(c.items)))
}

// Get 获取缓存项，如果项存在且未过期则返回true
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, found := c.items[key]
	if !found {
		c.metrics.RecordMiss()
		return nil, false
	}

	// 检查是否过期
	if item.IsExpired() {
		c.metrics.RecordMiss()
		return nil, false
	}

	c.metrics.RecordHit()
	return item.Value, true
}

// Delete 从缓存中删除一个项
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
	c.metrics.UpdateSize(int64(len(c.items)))
}

// DeleteExpired 删除所有过期的项
func (c *Cache) DeleteExpired() {
	now := time.Now().UnixNano()
	
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range c.items {
		if v.Expiration > 0 && now > v.Expiration {
			delete(c.items, k)
		}
	}
	
	c.metrics.UpdateSize(int64(len(c.items)))
}

// Clear 清空缓存
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]Item)
	c.metrics.UpdateSize(0)
}

// Count 返回缓存中的项数
func (c *Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.items)
}

// Items 返回缓存中的所有项的副本
func (c *Cache) Items() map[string]Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	items := make(map[string]Item, len(c.items))
	for k, v := range c.items {
		items[k] = v
	}

	return items
}

// GetMetrics 获取缓存指标
func (c *Cache) GetMetrics() map[string]interface{} {
	return c.metrics.GetStats()
}

// Stop 停止自动清理
func (c *Cache) Stop() {
	if c.cleanupInterval > 0 {
		c.stopCleanup <- true
	}
}