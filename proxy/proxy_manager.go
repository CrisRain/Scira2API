package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
	"scira2api/log"
	"scira2api/pkg/constants"
)

// 优化点: 组织化常量定义，按照功能分组
// 目的: 提高可读性，使常量含义更明确
// 预期效果: 更易于维护和理解的代码
const (
	// API相关常量
	proxyAPIBaseURL = "https://proxy.scdn.io/api/proxy_list.php"
	proxyVerifyURL  = "https://ip.gs/"
	proxyTimeout    = 10 * time.Second
	maxRetries      = 3
	retryDelay      = 2 * time.Second

	// 代理池相关常量
	minPoolSize      = 20               // 代理池最小数量，低于此值时触发刷新
	refreshInterval  = 60 * time.Minute // 定时刷新间隔
	validateInterval = 5 * time.Minute  // 代理验证间隔

	// 代理池持久化
	proxyPoolFile = "pool/proxy_pool.json" // 代理池持久化文件
	maxPagesFetch = 100                    // 从API获取的最大页数

	// 速率限制
	requestsPerSecond = 10 // 每秒请求数限制
	burstLimit        = 20 // 突发请求数限制
	maxConcurrent     = 20 // 最大并发验证数量
)

// 代理类型
const (
	ProxyTypeHTTP   = "HTTP"
	ProxyTypeHTTPS  = "HTTPS"
	ProxyTypeSOCKS4 = "SOCKS4"
	ProxyTypeSOCKS5 = "SOCKS5"
)

// ProxyListResponse API响应结构
type ProxyListResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Proxies []struct {
			ID           int    `json:"id"`
			IP           string `json:"ip"`
			Port         int    `json:"port"`
			Type         string `json:"type"`
			Country      string `json:"country"`
			ResponseTime int    `json:"response_time"`
			LastCheck    string `json:"last_check"`
			Status       int    `json:"status"`
		} `json:"proxies"`
		Pagination struct {
			CurrentPage   int `json:"current_page"`
			PerPage       int `json:"per_page"`
			TotalPages    int `json:"total_pages"`
			TotalFiltered int `json:"total_filtered"`
			TotalActive   int `json:"total_active"`
			Total         int `json:"total"`
		} `json:"pagination"`
		Stats struct {
			AvgTime int `json:"avg_time"`
		} `json:"stats"`
		Filters struct {
			Types     []string `json:"types"`
			Countries []string `json:"countries"`
		} `json:"filters"`
	} `json:"data"`
}

// Proxy 代表一个代理及其元数据
type Proxy struct {
	Address      string    // 代理地址，格式: "ip:port"
	LastVerify   time.Time // 上次验证时间
	FailCount    int       // 连续失败次数
	Type         string    // 代理类型: HTTP, HTTPS, SOCKS4, SOCKS5
	Country      string    // 代理所在国家/地区
	ResponseTime int       // 响应时间(毫秒)
}

// Manager 负责管理代理池
type Manager struct {
	httpClient   *http.Client
	proxyPool    []*Proxy     // 代理池
	mu           sync.RWMutex // 保护代理池的互斥锁
	ctx          context.Context
	cancel       context.CancelFunc
	refreshWg    sync.WaitGroup // 用于等待后台任务完成
	poolFilePath string         // 代理池持久化文件路径
}

// 优化点: 添加自定义错误类型
// 目的: 提高错误处理的精确性和可读性
// 预期效果: 更明确的错误原因，更容易诊断问题
var (
	ErrEmptyProxyPool = fmt.Errorf("代理池为空")
	ErrProxyAPIFailed = fmt.Errorf("代理API请求失败")
	ErrProxyRefresh   = fmt.Errorf("刷新代理池失败")
)

// NewManager 创建一个新的代理管理器实例并启动代理池维护
func NewManager() *Manager {
	// 优化点: 使用专门的函数创建HTTP客户端
	// 目的: 分离职责，提高代码清晰度
	// 预期效果: 更清晰的代码结构，易于维护
	client := createDefaultHTTPClient(proxyTimeout)
	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		httpClient:   client,
		ctx:          ctx,
		cancel:       cancel,
		poolFilePath: proxyPoolFile,
	}

	// 初始化代理池：先尝试从文件加载，然后再从API获取
	manager.initProxyPool()

	// 启动代理池维护任务
	manager.startPoolMaintenance()

	return manager
}

// 优化点: 抽取HTTP客户端创建逻辑
// 目的: 代码复用，统一配置
// 预期效果: 减少代码重复，易于维护
func createDefaultHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// initProxyPool 初始化代理池
func (m *Manager) initProxyPool() {
	log.Info("初始化代理池...")

	// 先尝试从文件加载代理池
	loaded := m.loadProxyPoolFromFile()

	// 添加本地代理到代理池
	m.addLocalProxies()

	// 如果从文件加载失败或加载的代理数量不足，则从API获取
	if !loaded || len(m.proxyPool) < minPoolSize {
		log.Info("从文件加载的代理不足，从API获取代理...")
		// fetchAndVerifyProxies 将直接修改 m.proxyPool 并按需保存
		err := m.fetchAndVerifyProxies()
		if err != nil {
			log.Error("从API获取代理过程中发生错误: %v", err)
			// 如果已经从文件加载了一些代理，并且API获取失败，则继续使用文件加载的代理
			if len(m.proxyPool) > 0 {
				log.Info("将使用现有 %d 个代理 (API获取失败或未添加新代理)", len(m.proxyPool))
				// 确保即使API获取失败，如果之前有从文件加载，也保存一次当前状态
				if loaded {
					m.saveProxyPoolToFile()
				}
				return
			}
			// 否则初始化失败
			return
		}
		log.Info("API代理获取流程完成，保存当前代理池状态...")
		m.saveProxyPoolToFile() // 保存最终状态，无论API是否添加了新代理
	}

	log.Info("代理池初始化完成，当前代理数量: %d", len(m.proxyPool))
}

// startPoolMaintenance 启动代理池维护任务
func (m *Manager) startPoolMaintenance() {
	// 启动刷新代理池的协程
	m.refreshWg.Add(1)
	go func() {
		defer m.refreshWg.Done()
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				log.Info("停止代理池刷新任务")
				return
			case <-ticker.C:
				m.refreshProxyPool()
			}
		}
	}()

	// 启动验证代理有效性的协程
	m.refreshWg.Add(1)
	go func() {
		defer m.refreshWg.Done()
		ticker := time.NewTicker(validateInterval)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				log.Info("停止代理验证任务")
				return
			case <-ticker.C:
				// 验证代理池，刷新会在validateProxies内部异步执行
				m.validateProxies()
			}
		}
	}()

	log.Info("代理池维护任务已启动")
}

// loadProxyPoolFromFile 从文件加载代理池
func (m *Manager) loadProxyPoolFromFile() bool {
	log.Info("尝试从文件加载代理池: %s", m.poolFilePath)

	// 检查文件是否存在
	if _, err := os.Stat(m.poolFilePath); os.IsNotExist(err) {
		log.Warn("代理池文件不存在: %s", m.poolFilePath)
		return false
	}

	data, err := os.ReadFile(m.poolFilePath)
	if err != nil {
		log.Warn("无法读取代理池文件: %v", err)
		return false
	}

	var proxies []*Proxy
	if err := json.Unmarshal(data, &proxies); err != nil {
		log.Error("解析代理池文件失败: %v", err)
		return false
	}

	if len(proxies) == 0 {
		log.Warn("代理池文件中没有代理")
		return false
	}

	// 优化点: 移除不必要的变量赋值
	// 目的: 简化代码
	// 预期效果: 更简洁的代码，减少变量

	m.mu.Lock()
	defer m.mu.Unlock()
	m.proxyPool = proxies

	log.Info("从文件成功加载 %d 个代理", len(proxies))
	return true
}

// saveProxyPoolToFile 保存代理池到文件
func (m *Manager) saveProxyPoolToFile() {
	log.Info("保存代理池到文件: %s", m.poolFilePath)

	m.mu.RLock()
	proxies := make([]*Proxy, len(m.proxyPool))
	copy(proxies, m.proxyPool)
	m.mu.RUnlock()

	if len(proxies) == 0 {
		log.Warn("代理池为空，不保存")
		return
	}

	// 优化点: 抽取确保目录存在的逻辑
	// 目的: 分离职责
	// 预期效果: 更清晰的代码
	if err := ensureDirectoryExists(m.poolFilePath); err != nil {
		log.Error("创建代理池目录失败: %v", err)
		return
	}

	data, err := json.MarshalIndent(proxies, "", "  ")
	if err != nil {
		log.Error("序列化代理池失败: %v", err)
		return
	}

	err = os.WriteFile(m.poolFilePath, data, 0644)
	if err != nil {
		log.Error("写入代理池文件失败: %v", err)
		return
	}

	log.Info("成功保存 %d 个代理到文件", len(proxies))
}

// 优化点: 抽取目录创建逻辑为单独函数
// 目的: 提高代码复用和可维护性
// 预期效果: 更清晰的代码结构，职责分明
func ensureDirectoryExists(filePath string) error {
	dir := filepath.Dir(filePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		log.Info("创建目录: %s", dir)
	}
	return nil
}

// Stop 停止代理池维护任务
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
		m.refreshWg.Wait()
		log.Info("代理池维护任务已停止")
	}
}

// GetProxy 从代理池中获取一个可用的代理地址
func (m *Manager) GetProxy() (string, error) {
	m.mu.RLock()

	if len(m.proxyPool) == 0 {
		m.mu.RUnlock()
		return m.refreshAndGetProxy()
	}

	// 优化点: 使用更智能的代理选择算法
	// 目的: 提高代理选择质量
	// 预期效果: 返回更可靠的代理
	proxy := m.selectBestProxy()
	if proxy == nil {
		// 如果没有合适的代理，则随机选择一个
		randIndex := rand.Intn(len(m.proxyPool))
		proxy = m.proxyPool[randIndex]
	}
	
	proxyType := proxy.Type
	address := proxy.Address
	m.mu.RUnlock()

	// 根据代理类型添加协议前缀
	formattedAddress := formatProxyAddress(address, proxyType)

	log.Info("从代理池获取代理: %s (类型: %s)", formattedAddress, proxyType)
	return formattedAddress, nil
}

// 优化点: 添加更智能的代理选择算法
// 目的: 提高代理可用性和性能
// 预期效果: 返回更稳定、响应更快的代理
func (m *Manager) selectBestProxy() *Proxy {
	candidates := make([]*Proxy, 0)
	
	// 选择验证成功且失败次数为0的代理
	for _, p := range m.proxyPool {
		if p.FailCount == 0 && !p.LastVerify.IsZero() {
			candidates = append(candidates, p)
		}
	}
	
	if len(candidates) == 0 {
		return nil
	}
	
	// 按响应时间排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ResponseTime < candidates[j].ResponseTime
	})
	
	// 从最快的前三个中随机选择一个
	candidateCount := len(candidates)
	if candidateCount > 3 {
		candidateCount = 3
	}
	return candidates[rand.Intn(candidateCount)]
}

// formatProxyAddress 根据代理类型格式化代理地址
func formatProxyAddress(address, proxyType string) string {
	if strings.Contains(address, "://") {
		return address
	}

	switch proxyType {
	case ProxyTypeHTTP:
		return "http://" + address
	case ProxyTypeHTTPS:
		return "https://" + address
	case ProxyTypeSOCKS4:
		return "socks4://" + address
	case ProxyTypeSOCKS5:
		return "socks5://" + address
	default:
		log.Warn("未知的代理类型: %s，地址: %s", proxyType, address)
		return address
	}
}

// refreshAndGetProxy 刷新代理池并获取一个代理
func (m *Manager) refreshAndGetProxy() (string, error) {
	log.Info("代理池为空，正在刷新...")
	if err := m.refreshProxyPool(); err != nil {
		return "", fmt.Errorf("刷新代理池失败: %w", err)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.proxyPool) == 0 {
		return "", ErrEmptyProxyPool
	}

	// 优化点: 使用相同的代理选择逻辑
	// 目的: 保持一致性，减少代码重复
	// 预期效果: 更可维护的代码
	proxy := m.selectBestProxy()
	if proxy == nil {
		// 如果没有合适的代理，则随机选择一个
		randIndex := rand.Intn(len(m.proxyPool))
		proxy = m.proxyPool[randIndex]
	}

	// 根据代理类型添加协议前缀
	formattedAddress := formatProxyAddress(proxy.Address, proxy.Type)

	log.Info("从刷新后的代理池获取代理: %s (类型: %s)", formattedAddress, proxy.Type)
	return formattedAddress, nil
}

// refreshProxyPool 刷新代理池
func (m *Manager) refreshProxyPool() error {
	log.Info("正在刷新代理池...")

	// fetchAndVerifyProxies 将直接修改 m.proxyPool 并按需保存
	err := m.fetchAndVerifyProxies()
	if err != nil {
		log.Error("刷新代理池时，获取新代理失败: %v", err)
		return ErrProxyRefresh
	}

	log.Info("代理池刷新流程执行完毕")
	// 无需在此处显式保存，fetchAndVerifyProxies 会在添加新代理时保存

	return nil
}

// validateProxies 验证代理池中的代理
func (m *Manager) validateProxies() {
	log.Info("开始验证代理池中的代理...")

	m.mu.RLock()
	poolSize := len(m.proxyPool)
	proxiesToValidate := make([]*Proxy, poolSize)
	copy(proxiesToValidate, m.proxyPool)
	m.mu.RUnlock()

	// 检查是否需要刷新代理池
	if poolSize < minPoolSize {
		log.Info("代理池数量 (%d) 低于最小值 (%d)，触发异步刷新", poolSize, minPoolSize)
		go m.refreshProxyPool()
	}

	// 优化点: 重构验证逻辑，减少复杂度
	// 目的: 提高代码可读性和可维护性
	// 预期效果: 更清晰的验证流程
	validatedCount, removedCount := m.processProxyValidation(proxiesToValidate)

	m.mu.RLock()
	finalPoolSize := len(m.proxyPool)
	m.mu.RUnlock()
	
	log.Info("代理池验证周期完成。重新验证成功: %d, 移除: %d, 当前池大小: %d", 
		validatedCount, removedCount, finalPoolSize)
}

// 优化点: 抽取代理验证处理逻辑
// 目的: 减少validateProxies函数复杂度
// 预期效果: 更清晰的代码结构，更好的错误处理
func (m *Manager) processProxyValidation(proxiesToValidate []*Proxy) (int, int) {
	var validatedCount, removedCount int

	for _, proxyInSnapshot := range proxiesToValidate {
		// 验证前获取当前池中代理的最新失败次数
		currentFailCount, found := m.getProxyFailCount(proxyInSnapshot.Address)
		if !found {
			log.Debug("代理 %s 在验证前已从池中移除，跳过验证", proxyInSnapshot.Address)
			continue
		}
		
		if m.verifyProxy(proxyInSnapshot.Address, proxyInSnapshot.Type) {
			// 验证成功
			validatedCount++
			m.updateProxyAfterSuccessfulValidation(proxyInSnapshot.Address)
		} else {
			// 验证失败
			removed := m.updateProxyAfterFailedValidation(proxyInSnapshot.Address, currentFailCount)
			if removed {
				removedCount++
			}
		}
	}
	
	return validatedCount, removedCount
}

// 优化点: 抽取获取代理失败次数逻辑
// 目的: 简化验证流程
// 预期效果: 更模块化的代码
func (m *Manager) getProxyFailCount(address string) (int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for _, p := range m.proxyPool {
		if p.Address == address {
			return p.FailCount, true
		}
	}
	return 0, false
}

// 优化点: 抽取验证成功后的更新逻辑
// 目的: 代码模块化
// 预期效果: 更清晰的责任划分
func (m *Manager) updateProxyAfterSuccessfulValidation(address string) {
	m.mu.Lock()
	updated := false
	for i, p := range m.proxyPool {
		if p.Address == address {
			m.proxyPool[i].LastVerify = time.Now()
			m.proxyPool[i].FailCount = 0
			updated = true
			log.Debug("代理 %s 重新验证成功", p.Address)
			break
		}
	}
	m.mu.Unlock()
	
	if updated {
		m.saveProxyPoolToFile()
	}
}

// 优化点: 抽取验证失败后的更新逻辑
// 目的: 代码模块化
// 预期效果: 更清晰的责任划分
func (m *Manager) updateProxyAfterFailedValidation(address string, currentFailCount int) bool {
	m.mu.Lock()
	removed := false
	updated := false
	
	tempPool := make([]*Proxy, 0, len(m.proxyPool))
	for _, p := range m.proxyPool {
		if p.Address == address {
			p.FailCount = currentFailCount + 1
			updated = true
			
			if p.FailCount >= 3 {
				removed = true
				log.Warn("代理 %s 连续验证失败 %d 次，从池中移除", p.Address, p.FailCount)
				// 不添加到临时池，相当于移除
			} else {
				log.Debug("代理 %s 验证失败，失败次数增加到 %d", p.Address, p.FailCount)
				tempPool = append(tempPool, p)
			}
		} else {
			tempPool = append(tempPool, p)
		}
	}
	
	if updated {
		m.proxyPool = tempPool
	}
	m.mu.Unlock()
	
	if updated {
		m.saveProxyPoolToFile()
	}
	
	return removed
}

// fetchAndVerifyProxies 从API获取代理，验证后直接添加到池中并保存
func (m *Manager) fetchAndVerifyProxies() error {
	// 获取代理列表
	apiProxies, err := m.fetchProxiesFromAPI()
	if err != nil {
		return err
	}

	if len(apiProxies) == 0 {
		log.Info("API未返回任何代理进行验证")
		return nil
	}

	log.Info("从API获取到 %d 个代理，开始验证并按需添加到池中...", len(apiProxies))

	// 优化点: 使用常量替换硬编码值
	// 目的: 提高可维护性
	// 预期效果: 更一致的代码风格
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var addedCount int32 // 原子计数器，统计成功添加的新代理数量

	for _, proxyFromAPI := range apiProxies {
		wg.Add(1)
		sem <- struct{}{} // 获取信号量

		go func(p *Proxy) {
			defer func() {
				<-sem // 释放信号量
				wg.Done()
			}()

			// 优化点: 抽取验证和添加逻辑
			// 目的: 简化代码，提高可读性
			// 预期效果: 更清晰的代码结构
			if added := m.verifyAndAddProxy(p); added {
				atomic.AddInt32(&addedCount, 1)
			}
		}(proxyFromAPI)
	}

	wg.Wait()
	close(sem)

	finalAddedCount := atomic.LoadInt32(&addedCount)
	if finalAddedCount > 0 {
		log.Info("从API获取并验证流程完成，共添加了 %d 个新代理到池中。", finalAddedCount)
	} else {
		log.Info("从API获取并验证流程完成，没有新代理被添加到池中。")
	}
	return nil
}

// 优化点: 抽取代理验证和添加逻辑
// 目的: 简化fetchAndVerifyProxies函数
// 预期效果: 更简洁清晰的代码
func (m *Manager) verifyAndAddProxy(p *Proxy) bool {
	if !m.verifyProxy(p.Address, p.Type) {
		log.Debug("API提供的代理验证失败: %s (类型: %s)", p.Address, p.Type)
		return false
	}
	
	p.LastVerify = time.Now()
	p.FailCount = 0

	m.mu.Lock()
	isExisting := m.isProxyInPool(p.Address)
	if !isExisting {
		m.proxyPool = append(m.proxyPool, p)
		log.Info("新代理验证成功并已添加: %s (类型: %s, 响应时间: %dms)", p.Address, p.Type, p.ResponseTime)
	} else {
		log.Debug("已验证代理 %s 已存在于池中，未重复添加", p.Address)
	}
	m.mu.Unlock()

	if !isExisting {
		m.saveProxyPoolToFile()
		return true
	}
	
	return false
}

// 优化点: 抽取代理池检查逻辑
// 目的: 提高代码复用
// 预期效果: 减少重复代码
func (m *Manager) isProxyInPool(address string) bool {
	for _, existingProxy := range m.proxyPool {
		if existingProxy.Address == address {
			return true
		}
	}
	return false
}

// 添加本地代理到代理池 - 当前已禁用预定义本地代理
func (m *Manager) addLocalProxies() {
	// 本地代理已禁用
	log.Info("本地代理功能已禁用")
}

// fetchProxiesFromAPI 从指定的API批量获取代理
func (m *Manager) fetchProxiesFromAPI() ([]*Proxy, error) {
	// 优化点: 减少不必要的日志记录
	// 目的: 减少日志冗余
	// 预期效果: 更简洁的日志输出
	log.Info("开始从API获取代理列表...")

	// 优化点: 抽取限速器创建逻辑
	// 目的: 简化主函数，增强可读性
	// 预期效果: 更清晰的代码结构
	rateLimiter, done := createRateLimiter(requestsPerSecond, burstLimit)
	defer close(done)

	var allProxies []*Proxy
	var successCount int // 成功获取的页数

	// 优化点: 抽取第一页获取逻辑
	// 目的: 减少函数长度，提高可维护性
	// 预期效果: 更模块化的代码
	firstPageProxies, totalPages, err := m.fetchFirstPageProxies(rateLimiter)
	if err != nil {
		return nil, err
	}
	
	// 添加第一页数据到结果
	allProxies = append(allProxies, firstPageProxies...)
	if len(firstPageProxies) > 0 {
		successCount++
	}

	log.Info("第1页获取到%d个有效代理", len(firstPageProxies))

	// 如果有多页，获取剩余页面（从第2页开始）
	if totalPages > 1 {
		remainingProxies, pageSuccessCount := m.fetchRemainingPages(rateLimiter, totalPages)
		allProxies = append(allProxies, remainingProxies...)
		successCount += pageSuccessCount
	}

	// 检查是否至少有一个页面成功
	if successCount == 0 {
		return nil, fmt.Errorf("所有页面获取失败，未能获得任何有效代理")
	}

	// 优化点: 抽取代理去重逻辑
	// 目的: 简化主函数
	// 预期效果: 更清晰的代码结构
	uniqueProxies := m.deduplicateProxies(allProxies)

	if len(uniqueProxies) == 0 {
		return nil, fmt.Errorf("未找到有效的代理")
	}

	// 记录代理统计信息
	m.logProxyStatistics(uniqueProxies)

	return uniqueProxies, nil
}

// 优化点: 创建速率限制器函数
// 目的: 简化代码，提高可读性
// 预期效果: 更清晰的代码结构
func createRateLimiter(requestsPerSecond, burstLimit int) (chan struct{}, chan struct{}) {
	rateLimiter := make(chan struct{}, burstLimit)
	done := make(chan struct{})
	
	// 启动限速协程
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(requestsPerSecond))
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case rateLimiter <- struct{}{}:
					// 添加令牌到限速器
				default:
					// 限速器已满，跳过
				}
			}
		}
	}()

	// 初始化限速器，预先填充burstLimit个令牌
	for i := 0; i < burstLimit; i++ {
		select {
		case rateLimiter <- struct{}{}:
		default:
		}
	}
	
	return rateLimiter, done
}

// 优化点: 抽取第一页获取逻辑
// 目的: 减少主函数复杂度
// 预期效果: 更清晰的代码组织
func (m *Manager) fetchFirstPageProxies(rateLimiter chan struct{}) ([]*Proxy, int, error) {
	log.Info("获取第一页代理以确定总页数...")
	
	// 获取限速令牌
	<-rateLimiter
	
	// 发送请求获取第一页
	currUA := constants.GetRandomUserAgent()
	apiURL := proxyAPIBaseURL + "?page=1&per_page=100&type=socks5"
	
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Warn("创建第1页代理请求失败: %v", err)
		return nil, 0, err
	}
	req.Header.Set("User-Agent", currUA)
	
	// 优化点: 抽取创建API请求客户端的逻辑
	// 目的: 减少代码重复
	// 预期效果: 更简洁的代码
	clientToUse := m.createAPIRequestClient(apiURL)
	
	// 发送请求
	resp, err := clientToUse.Do(req)
	if err != nil {
		log.Warn("请求第1页代理失败: %v", err)
		return nil, 0, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		log.Warn("第1页返回错误状态码: %d", resp.StatusCode)
		return nil, 0, fmt.Errorf("API返回错误状态码: %d", resp.StatusCode)
	}
	
	// 解析第一页响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warn("读取第1页响应失败: %v", err)
		return nil, 0, err
	}
	
	var proxyListResp ProxyListResponse
	if err := json.Unmarshal(respBody, &proxyListResp); err != nil {
		log.Warn("解析第1页响应失败: %v", err)
		return nil, 0, err
	}
	
	// 验证响应
	if !proxyListResp.Success {
		log.Warn("第1页返回success=false")
		return nil, 0, fmt.Errorf("API返回success=false")
	}
	
	// 确定实际总页数
	totalPages := proxyListResp.Data.Pagination.TotalPages
	if totalPages <= 0 {
		totalPages = 1 // 如果API没返回有效的总页数，就只处理第一页
	} else if totalPages > maxPagesFetch {
		totalPages = maxPagesFetch // 限制最大页数
	}
	
	log.Info("API返回总页数: %d, 将爬取页数: %d", proxyListResp.Data.Pagination.TotalPages, totalPages)
	
	// 处理第一页数据
	return m.parseProxyListResponse(&proxyListResp), totalPages, nil
}

// 优化点: 抽取创建API请求客户端的逻辑
// 目的: 减少代码重复
// 预期效果: 更简洁、可复用的代码
func (m *Manager) createAPIRequestClient(apiURL string) *http.Client {
	clientToUse := m.httpClient // 默认使用manager的httpClient
	
	m.mu.RLock()
	poolNotEmpty := len(m.proxyPool) > 0
	m.mu.RUnlock()
	
	if poolNotEmpty {
		proxyAddrToUse, err_get_proxy := m.GetProxy() // GetProxy内部已经处理了锁
		if err_get_proxy == nil && proxyAddrToUse != "" {
			parsedProxyURL, err_parse_proxy := url.Parse(proxyAddrToUse)
			if err_parse_proxy == nil {
				log.Info("尝试使用池中代理 %s 爬取API %s", proxyAddrToUse, apiURL)
				
				client, err := m.createProxyClient(parsedProxyURL)
				if err == nil {
					clientToUse = client
				}
			} else {
				log.Warn("解析从池中获取的代理地址 %s 失败: %v, 将不使用此代理", proxyAddrToUse, err_parse_proxy)
			}
		} else if err_get_proxy != nil {
			log.Warn("从池中获取代理失败: %v, 将不使用代理爬取API", err_get_proxy)
		}
	} else {
		log.Info("代理池为空，直接爬取API %s", apiURL)
	}
	
	return clientToUse
}

// 优化点: 抽取代理客户端创建逻辑
// 目的: 简化代码，增强可读性
// 预期效果: 更清晰的代码结构
func (m *Manager) createProxyClient(proxyURL *url.URL) (*http.Client, error) {
	tempTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	
	// 根据代理类型配置Transport
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		tempTransport.Proxy = http.ProxyURL(proxyURL)
	case "socks5":
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err != nil {
			return nil, err
		}
		tempTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	default:
		return nil, fmt.Errorf("不支持的代理类型: %s", proxyURL.Scheme)
	}
	
	// 只有在成功配置代理时才创建新client
	if tempTransport.Proxy != nil || tempTransport.DialContext != nil {
		return &http.Client{
			Transport: tempTransport,
			Timeout:   m.httpClient.Timeout,
		}, nil
	}
	
	return nil, fmt.Errorf("未能配置代理客户端")
}

// 优化点: 抽取代理响应解析逻辑
// 目的: 减少代码重复
// 预期效果: 更易维护的代码
func (m *Manager) parseProxyListResponse(response *ProxyListResponse) []*Proxy {
	var proxies []*Proxy
	
	for _, proxyInfo := range response.Data.Proxies {
		if proxyInfo.Status == 1 && proxyInfo.IP != "" && proxyInfo.Port > 0 {
			proxyType := strings.ToUpper(proxyInfo.Type)
			
			// 验证代理类型是否为已知类型
			isValidType := false
			switch proxyType {
			case ProxyTypeHTTP, ProxyTypeHTTPS, ProxyTypeSOCKS4, ProxyTypeSOCKS5:
				isValidType = true
			}
			
			if !isValidType {
				log.Warn("跳过未知类型(%s)的代理: %s:%d", proxyType, proxyInfo.IP, proxyInfo.Port)
				continue
			}
			
			proxy := &Proxy{
				Address:      fmt.Sprintf("%s:%d", proxyInfo.IP, proxyInfo.Port),
				LastVerify:   time.Now(),
				Type:         proxyType,
				Country:      proxyInfo.Country,
				ResponseTime: proxyInfo.ResponseTime,
			}
			proxies = append(proxies, proxy)
		}
	}
	
	return proxies
}

// 优化点: 抽取剩余页面获取逻辑
// 目的: 减少主函数复杂度
// 预期效果: 更清晰的代码组织
func (m *Manager) fetchRemainingPages(rateLimiter chan struct{}, totalPages int) ([]*Proxy, int) {
	var proxies []*Proxy
	var successCount int
	
	for pageNum := 2; pageNum <= totalPages; pageNum++ {
		// 请求前获取限速令牌
		<-rateLimiter
		
		// 优化点: 抽取单页获取逻辑
		// 目的: 减少代码重复
		// 预期效果: 更简洁的代码
		pageProxies, success := m.fetchSinglePage(pageNum)
		
		if success {
			proxies = append(proxies, pageProxies...)
			successCount++
			log.Info("第%d页获取到%d个有效代理", pageNum, len(pageProxies))
		} else {
			log.Warn("第%d页未获取到有效代理", pageNum)
		}
	}
	
	return proxies, successCount
}

// 优化点: 抽取单页获取逻辑
// 目的: 减少代码重复
// 预期效果: 更清晰的代码结构
func (m *Manager) fetchSinglePage(pageNum int) ([]*Proxy, bool) {
	// 发送请求，每个请求使用新的随机UA
	currUA := constants.GetRandomUserAgent()
	
	// 构建URL和请求
	pageAPIURL := fmt.Sprintf("%s?page=%d&per_page=100&type=socks5", proxyAPIBaseURL, pageNum)
	req, err := http.NewRequest("GET", pageAPIURL, nil)
	if err != nil {
		log.Warn("创建第%d页代理请求失败: %v", pageNum, err)
		return nil, false
	}
	req.Header.Set("User-Agent", currUA)
	
	// 创建客户端
	clientToUseForPage := m.createAPIRequestClient(pageAPIURL)
	
	// 发送请求
	resp, err := clientToUseForPage.Do(req)
	if err != nil {
		log.Warn("请求第%d页代理失败: %v", pageNum, err)
		return nil, false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		log.Warn("第%d页返回错误状态码: %d", pageNum, resp.StatusCode)
		return nil, false
	}
	
	// 解析响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warn("读取第%d页响应失败: %v", pageNum, err)
		return nil, false
	}
	
	var proxyListResp ProxyListResponse
	if err := json.Unmarshal(respBody, &proxyListResp); err != nil {
		log.Warn("解析第%d页响应失败: %v", pageNum, err)
		return nil, false
	}
	
	// 验证响应
	if !proxyListResp.Success {
		log.Warn("第%d页返回success=false", pageNum)
		return nil, false
	}
	
	// 检查代理数据
	if len(proxyListResp.Data.Proxies) == 0 {
		log.Warn("第%d页代理数据为空", pageNum)
		return nil, false
	}
	
	// 解析响应
	pageProxies := m.parseProxyListResponse(&proxyListResp)
	return pageProxies, len(pageProxies) > 0
}

// 优化点: 抽取代理去重逻辑
// 目的: 简化主函数
// 预期效果: 更清晰的代码结构
func (m *Manager) deduplicateProxies(proxies []*Proxy) []*Proxy {
	uniqueProxies := make(map[string]*Proxy)
	for _, proxy := range proxies {
		uniqueProxies[proxy.Address] = proxy
	}
	
	// 转回切片
	result := make([]*Proxy, 0, len(uniqueProxies))
	for _, proxy := range uniqueProxies {
		result = append(result, proxy)
	}
	
	return result
}

// 优化点: 抽取统计信息记录逻辑
// 目的: 简化主函数
// 预期效果: 更清晰的代码结构
func (m *Manager) logProxyStatistics(proxies []*Proxy) {
	typeCount := make(map[string]int)
	countryCount := make(map[string]int)
	
	for _, p := range proxies {
		typeCount[p.Type]++
		countryCount[p.Country]++
	}
	
	log.Info("共获取到 %d 个有效代理，类型分布: %v", len(proxies), typeCount)
	log.Debug("国家/地区分布: %v", countryCount)
}

// verifyProxy 验证代理是否有效
func (m *Manager) verifyProxy(proxyAddr string, proxyType string) bool {
	// 直接使用传入的代理类型
	if proxyType == "" {
		log.Warn("代理 %s 无类型信息，无法验证", proxyAddr)
		return false
	}

	// 优化点: 减少调试日志
	// 目的: 减少日志冗余
	// 预期效果: 更清晰的日志输出

	// 根据代理类型添加正确的协议前缀
	if !strings.Contains(proxyAddr, "://") {
		// 使用formatProxyAddress统一处理地址格式化
		proxyAddr = formatProxyAddress(proxyAddr, proxyType)

		// 如果地址没有协议前缀（可能是未知类型导致的），则无法验证
		if !strings.Contains(proxyAddr, "://") {
			log.Warn("代理 %s 添加协议前缀失败，无法验证", proxyAddr)
			return false
		}
	}

	// 使用HTTPS验证URL验证所有类型的代理
	verifyURL := proxyVerifyURL
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		log.Error("解析代理地址 %s 失败: %v", proxyAddr, err)
		return false
	}

	// 获取代理主机IP
	proxyHost := proxyURL.Hostname()

	// 优化点: 抽取代理验证客户端创建逻辑
	// 目的: 简化代码，增强可读性
	// 预期效果: 更清晰的代码结构
	client, err := m.createVerificationClient(proxyURL)
	if err != nil {
		log.Error("创建验证客户端失败: %v", err)
		return false
	}

	// 创建请求
	req, err := http.NewRequest("GET", verifyURL, nil)
	if err != nil {
		log.Error("创建验证请求失败: %v", err)
		return false
	}

	// 添加随机User-Agent
	randomUA := constants.GetRandomUserAgent()
	req.Header.Set("User-Agent", randomUA)

	// 设置更长的超时时间进行验证
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		log.Warn("通过代理 %s 验证IP失败: %v (类型: %s)", proxyAddr, err, proxyType)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn("通过代理 %s 验证IP返回状态码: %d", proxyAddr, resp.StatusCode)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warn("读取代理 %s 验证响应体失败: %v", proxyAddr, err)
		return false
	}

	// 获取验证返回的IP
	returnedIP := strings.TrimSpace(string(body))
	if returnedIP == "" {
		log.Warn("通过代理 %s 验证IP返回空", proxyAddr)
		return false
	}

	// 核心验证逻辑：当返回的IP与代理主机IP相同时，视为代理有效
	isValid := returnedIP == proxyHost
	if isValid {
		log.Info("代理 %s 验证成功：返回IP(%s)与代理主机IP匹配", proxyAddr, returnedIP)
	} else {
		log.Warn("代理 %s 验证失败：返回IP(%s)与代理主机IP(%s)不匹配", proxyAddr, returnedIP, proxyHost)
	}

	return isValid
}

// 优化点: 抽取验证客户端创建逻辑
// 目的: 简化verifyProxy函数
// 预期效果: 更清晰的代码结构
func (m *Manager) createVerificationClient(proxyURL *url.URL) (*http.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// 根据代理类型配置Transport
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DialContext = nil // 确保不使用SOCKS dialer
	case "socks5":
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("为SOCKS5代理创建dialer失败: %v", err)
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
		transport.Proxy = nil // 确保不使用HTTP代理
	default:
		return nil, fmt.Errorf("不支持的代理协议: %s", proxyURL.Scheme)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   proxyTimeout,
	}, nil
}

// ForceRefreshProxy 强制刷新代理池并返回一个新代理
func (m *Manager) ForceRefreshProxy() (string, error) {
	log.Info("正在强制刷新代理池...")

	// 优化点: 抽取临时代理获取逻辑
	// 目的: 简化函数，提高可读性
	// 预期效果: 更清晰的代码结构
	tempProxy := m.getTemporaryProxy()

	// 启动异步刷新
	refreshDone := make(chan struct{})
	var refreshErr error
	go func() {
		defer close(refreshDone)
		refreshErr = m.refreshProxyPool()
	}()

	// 等待刷新完成或超时
	select {
	case <-refreshDone:
		if refreshErr != nil {
			log.Warn("代理池刷新失败: %v, 尝试使用现有代理", refreshErr)
			// 如果刷新失败但有临时代理，则使用临时代理
			if tempProxy != nil {
				formattedAddress := formatProxyAddress(tempProxy.Address, tempProxy.Type)
				log.Info("返回现有代理: %s (类型: %s)", formattedAddress, tempProxy.Type)
				return formattedAddress, nil
			}
			return "", fmt.Errorf("强制刷新代理池失败: %w", refreshErr)
		}
	case <-time.After(5 * time.Second):
		log.Warn("代理池刷新超时，尝试使用现有代理")
		// 如果刷新超时但有临时代理，则使用临时代理
		if tempProxy != nil {
			formattedAddress := formatProxyAddress(tempProxy.Address, tempProxy.Type)
			log.Info("返回现有代理: %s (类型: %s)", formattedAddress, tempProxy.Type)
			return formattedAddress, nil
		}
	}

	// 优化点: 使用统一的最佳代理选择逻辑
	// 目的: 保持一致性
	// 预期效果: 更高质量的代理选择
	return m.GetProxy()
}

// 优化点: 抽取临时代理获取逻辑
// 目的: 简化ForceRefreshProxy函数
// 预期效果: 更清晰的代码结构
func (m *Manager) getTemporaryProxy() *Proxy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	currentPoolSize := len(m.proxyPool)
	if currentPoolSize == 0 {
		return nil
	}
	
	// 优先选择最近验证成功且失败次数为0的代理
	validProxies := make([]*Proxy, 0)
	for _, p := range m.proxyPool {
		if p.FailCount == 0 {
			validProxies = append(validProxies, p)
		}
	}
	
	if len(validProxies) > 0 {
		// 从有效代理中随机选择一个
		return validProxies[rand.Intn(len(validProxies))]
	} 
	
	// 如果没有验证成功的代理，从所有代理中随机选择
	return m.proxyPool[rand.Intn(currentPoolSize)]
}
