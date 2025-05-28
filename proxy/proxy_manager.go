package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"scira2api/log"
	"scira2api/pkg/constants"

	"github.com/go-resty/resty/v2"
)

const (
	// 代理API基础URL
	proxyAPIBaseURL = "https://proxy.scdn.io/api/proxy_list.php"
	// 验证URL (只使用HTTPS版本)
	proxyVerifyURL  = "https://ip.gs/"
	proxyTimeout        = 10 * time.Second
	maxRetries          = 3
	retryDelay          = 2 * time.Second

	// 代理池相关常量
	minPoolSize      = 20              // 代理池最小数量，低于此值时触发刷新
	refreshInterval  = 5 * time.Minute // 定时刷新间隔
	validateInterval = 2 * time.Minute // 代理验证间隔

	// 代理池持久化
	proxyPoolFile = "pool/proxy_pool.json" // 代理池持久化文件
	maxPagesFetch = 100                    // 从API获取的最大页数
	
	// 速率限制
	requestsPerSecond = 2                  // 每秒请求数限制
	burstLimit        = 5                  // 突发请求数限制
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

// Manager 负责管理SOCKS5代理池
type Manager struct {
	httpClient   *resty.Client
	proxyPool    []*Proxy     // 代理池
	mu           sync.RWMutex // 保护代理池的互斥锁
	ctx          context.Context
	cancel       context.CancelFunc
	refreshWg    sync.WaitGroup // 用于等待后台任务完成
	poolFilePath string         // 代理池持久化文件路径
}

// NewManager 创建一个新的代理管理器实例并启动代理池维护
func NewManager() *Manager {
	client := resty.New().
		SetTimeout(proxyTimeout).
		SetRetryCount(maxRetries).
		SetRetryWaitTime(retryDelay).
		SetRetryMaxWaitTime(20*time.Second).
		SetHeader("User-Agent", constants.GetRandomUserAgent())

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
		proxies, err := m.fetchAndVerifyProxies()
		if err != nil {
			log.Error("从API获取代理失败: %v", err)
			// 如果已经从文件加载了一些代理，就使用这些代理
			if len(m.proxyPool) > 0 {
				log.Info("将使用从文件加载的 %d 个代理", len(m.proxyPool))
				return
			}
			// 否则初始化失败
			return
		}

		m.mu.Lock()
		// 如果已经有一些代理，合并新获取的代理
		if len(m.proxyPool) > 0 {
			existingProxies := make(map[string]bool)
			for _, p := range m.proxyPool {
				existingProxies[p.Address] = true
			}

			for _, newProxy := range proxies {
				if !existingProxies[newProxy.Address] {
					m.proxyPool = append(m.proxyPool, newProxy)
				}
			}
		} else {
			// 如果没有现有代理，直接设置
			m.proxyPool = proxies
		}
		m.mu.Unlock()

		// 保存代理池到文件
		m.saveProxyPoolToFile()
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

	// 不再过滤本地代理，保留所有加载的代理
	validProxies := proxies

	if len(validProxies) == 0 {
		log.Warn("所有加载的代理均已过期")
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.proxyPool = validProxies

	log.Info("从文件成功加载 %d 个代理", len(validProxies))
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

	// 确保代理池文件目录存在
	dir := filepath.Dir(m.poolFilePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Error("创建代理池目录失败: %v", err)
			return
		}
		log.Info("创建代理池目录: %s", dir)
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

	// 随机选择一个代理
	randIndex := rand.Intn(len(m.proxyPool))
	proxy := m.proxyPool[randIndex]
	proxyType := proxy.Type
	address := proxy.Address
	m.mu.RUnlock()

	// 根据代理类型添加协议前缀
	formattedAddress := formatProxyAddress(address, proxyType)

	log.Info("从代理池获取代理: %s (类型: %s)", formattedAddress, proxyType)
	return formattedAddress, nil
}

// formatProxyAddress 根据代理类型格式化代理地址
func formatProxyAddress(address, proxyType string) string {
	if strings.Contains(address, "://") {
		return address
	}

	switch proxyType {
	case ProxyTypeHTTP, ProxyTypeHTTPS:
		// HTTP和HTTPS类型的代理都使用http://前缀，它们可以处理http和https请求
		return "http://" + address
	case ProxyTypeSOCKS4:
		return "socks4://" + address
	case ProxyTypeSOCKS5:
		return "socks5://" + address
	default:
		// 未知类型，返回原始地址，调用方需要处理
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
		return "", fmt.Errorf("代理池为空")
	}

	// 随机选择一个代理
	randIndex := rand.Intn(len(m.proxyPool))
	proxy := m.proxyPool[randIndex]
	proxyType := proxy.Type
	address := proxy.Address

	// 根据代理类型添加协议前缀
	formattedAddress := formatProxyAddress(address, proxyType)

	log.Info("从刷新后的代理池获取代理: %s (类型: %s)", formattedAddress, proxyType)
	return formattedAddress, nil
}

// refreshProxyPool 刷新代理池
func (m *Manager) refreshProxyPool() error {
	log.Info("正在刷新代理池...")

	// 获取并验证新代理
	newProxies, err := m.fetchAndVerifyProxies()
	if err != nil {
		log.Error("获取新代理失败: %v", err)
		return err
	}

	if len(newProxies) == 0 {
		log.Warn("未获取到任何有效代理")
		return fmt.Errorf("未获取到有效代理")
	}

	// 使用读写锁保护代理池更新，减少锁持有时间
	// 先复制当前代理池中的地址到map中
	m.mu.RLock()
	existingProxies := make(map[string]bool, len(m.proxyPool))
	for _, p := range m.proxyPool {
		existingProxies[p.Address] = true
	}
	m.mu.RUnlock()

	// 筛选出不重复的新代理
	var uniqueNewProxies []*Proxy
	for _, newProxy := range newProxies {
		if !existingProxies[newProxy.Address] {
			uniqueNewProxies = append(uniqueNewProxies, newProxy)
		}
	}

	// 只在需要添加新代理时获取写锁
	if len(uniqueNewProxies) > 0 {
		m.mu.Lock()
		// 再次检查是否有重复代理，因为在我们获取锁之前可能有其他协程修改了代理池
		currentAddresses := make(map[string]bool)
		for _, p := range m.proxyPool {
			currentAddresses[p.Address] = true
		}

		// 添加真正不重复的代理
		var addedCount int
		for _, newProxy := range uniqueNewProxies {
			if !currentAddresses[newProxy.Address] {
				m.proxyPool = append(m.proxyPool, newProxy)
				addedCount++
			}
		}
		poolSize := len(m.proxyPool)
		m.mu.Unlock()

		log.Info("代理池刷新完成，添加了 %d 个新代理，当前代理数量: %d", addedCount, poolSize)
	} else {
		log.Info("代理池刷新完成，没有新的代理添加")
	}

	// 保存更新后的代理池到文件
	go m.saveProxyPoolToFile()

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

	// 检查是否需要刷新代理池，无论验证结果如何
	// 将刷新代理池的操作放在验证之前并异步执行，实现同步进行
	if poolSize < minPoolSize {
		log.Info("代理池数量 (%d) 低于最小值 (%d)，触发异步刷新", poolSize, minPoolSize)
		go m.refreshProxyPool()
	}

	var validProxies []*Proxy
	var wg sync.WaitGroup
	var mu sync.Mutex // 保护validProxies

	// 并发验证代理
	for _, proxy := range proxiesToValidate {
		wg.Add(1)
		go func(p *Proxy) {
			defer wg.Done()
			if m.verifyProxy(p.Address, p.Type) {
				mu.Lock()
				p.LastVerify = time.Now()
				p.FailCount = 0
				validProxies = append(validProxies, p)
				mu.Unlock()
			} else {
				mu.Lock()
				p.FailCount++
				// 如果失败次数少于3次，仍然保留
				if p.FailCount < 3 {
					validProxies = append(validProxies, p)
				} else {
					log.Warn("代理 %s 连续验证失败 %d 次，移除", p.Address, p.FailCount)
				}
				mu.Unlock()
			}
		}(proxy)
	}

	wg.Wait()

	m.mu.Lock()
	m.proxyPool = validProxies
	newPoolSize := len(m.proxyPool)
	m.mu.Unlock()

	log.Info("代理验证完成，有效代理数量: %d", newPoolSize)

	// 无论代理池数量如何，都保存更新后的代理池到文件
	go m.saveProxyPoolToFile()
}

// fetchAndVerifyProxies 批量获取并验证代理
func (m *Manager) fetchAndVerifyProxies() ([]*Proxy, error) {
	// 获取代理列表
	proxyList, err := m.fetchProxiesFromAPI()
	if err != nil {
		return nil, err
	}

	log.Info("从API获取到 %d 个代理，开始验证...", len(proxyList))

	// 使用通道控制并发数
	maxConcurrent := 20 // 最大并发验证数量
	sem := make(chan struct{}, maxConcurrent)
	
	var validProxies []*Proxy
	var wg sync.WaitGroup
	var mu sync.Mutex // 保护validProxies

	// 并发验证代理
	for _, proxy := range proxyList {
		wg.Add(1)
		sem <- struct{}{} // 获取信号量，限制并发数
		
		go func(p *Proxy) {
			defer func() {
				<-sem // 释放信号量
				wg.Done()
			}()
			
			// 将代理类型信息传递给verifyProxy函数
			if m.verifyProxy(p.Address, p.Type) {
				mu.Lock()
				p.LastVerify = time.Now() // 更新验证时间
				p.FailCount = 0           // 重置失败计数
				validProxies = append(validProxies, p)
				mu.Unlock()
				log.Debug("代理验证成功: %s (类型: %s)", p.Address, p.Type)
			} else {
				log.Debug("代理验证失败: %s (类型: %s)", p.Address, p.Type)
			}
		}(proxy)
	}

	wg.Wait()
	close(sem)

	// 按响应时间排序，优先使用响应快的代理
	sort.Slice(validProxies, func(i, j int) bool {
		return validProxies[i].ResponseTime < validProxies[j].ResponseTime
	})

	validCount := len(validProxies)
	totalCount := len(proxyList)
	successRate := float64(validCount) / float64(totalCount) * 100
	
	log.Info("代理验证完成，有效代理数量: %d/%d (%.1f%%)", validCount, totalCount, successRate)
	
	return validProxies, nil
}

// 添加本地代理到代理池 - 当前已禁用预定义本地代理
func (m *Manager) addLocalProxies() {
	// 本地代理已禁用
	log.Info("本地代理功能已禁用")
}

// fetchProxiesFromAPI 从指定的API批量获取代理
func (m *Manager) fetchProxiesFromAPI() ([]*Proxy, error) {
	// 设置随机User-Agent
	randomUA := constants.GetRandomUserAgent()
	m.httpClient.SetHeader("User-Agent", randomUA)
	log.Info("获取代理使用UA: %s", randomUA)

	// 获取多页代理数据
	var allProxies []*Proxy
	
	// 创建速率限制器
	rateLimiter := make(chan struct{}, burstLimit)
	// 启动限速协程
	done := make(chan struct{})
	defer close(done)
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

	var wg sync.WaitGroup
	var mu sync.Mutex      // 保护allProxies
	var successCount int32 // 成功获取的页数

	// 先获取第一页以确定总页数
	log.Info("先获取第一页代理以确定总页数...")
	
	// 获取限速令牌
	<-rateLimiter
	
	// 构建第一页请求参数
	params := map[string]string{
		"page":     "1",
		"per_page": "100",
		"type":     "socks5",
	}

	// 发送请求获取第一页
	currUA := constants.GetRandomUserAgent()
	resp, err := m.httpClient.R().
		SetQueryParams(params).
		SetHeader("User-Agent", currUA).
		Get(proxyAPIBaseURL)

	if err != nil {
		log.Warn("请求第1页代理失败: %v", err)
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		log.Warn("第1页返回错误状态码: %d", resp.StatusCode())
		return nil, fmt.Errorf("API返回错误状态码: %d", resp.StatusCode())
	}

	// 解析第一页响应
	respBody := resp.Body()
	var proxyListResp ProxyListResponse
	if err := json.Unmarshal(respBody, &proxyListResp); err != nil {
		log.Warn("解析第1页响应失败: %v", err)
		return nil, err
	}

	// 验证响应
	if !proxyListResp.Success {
		log.Warn("第1页返回success=false")
		return nil, fmt.Errorf("API返回success=false")
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
	var firstPageProxies []*Proxy
	for _, proxyInfo := range proxyListResp.Data.Proxies {
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
			firstPageProxies = append(firstPageProxies, proxy)
		}
	}
	
	// 添加第一页数据到结果
	mu.Lock()
	allProxies = append(allProxies, firstPageProxies...)
	if len(firstPageProxies) > 0 {
		atomic.AddInt32(&successCount, 1)
	}
	mu.Unlock()
	
	log.Info("第1页获取到%d个有效代理", len(firstPageProxies))

	// 如果有多页，并发爬取剩余页面（从第2页开始）
	if totalPages > 1 {
		for page := 2; page <= totalPages; page++ {
			wg.Add(1)
			go func(pageNum int) {
				defer wg.Done()
				
				// 请求前获取限速令牌
				log.Debug("等待获取页面%d的限速令牌", pageNum)
				<-rateLimiter
				log.Debug("获得页面%d的限速令牌，开始请求", pageNum)

				// 构建请求URL及参数
				params := map[string]string{
					"page":     fmt.Sprintf("%d", pageNum),
					"per_page": "100", // 每页获取100个代理
					"type":     "socks5", // 仅获取SOCKS5类型的代理
				}

				// 发送请求，每个请求使用新的随机UA
				currUA := constants.GetRandomUserAgent()
				resp, err := m.httpClient.R().
					SetQueryParams(params).
					SetHeader("User-Agent", currUA).
					Get(proxyAPIBaseURL)

				if err != nil {
					log.Warn("请求第%d页代理失败: %v", pageNum, err)
					return
				}

				if resp.StatusCode() != http.StatusOK {
					log.Warn("第%d页返回错误状态码: %d", pageNum, resp.StatusCode())
					return
				}

				// 解析响应
				respBody := resp.Body()
				var proxyListResp ProxyListResponse
				if err := json.Unmarshal(respBody, &proxyListResp); err != nil {
					log.Warn("解析第%d页响应失败: %v", pageNum, err)
					return
				}

				// 验证响应
				if !proxyListResp.Success {
					log.Warn("第%d页返回success=false", pageNum)
					return
				}

				// 检查代理数据
				if len(proxyListResp.Data.Proxies) == 0 {
					log.Warn("第%d页代理数据为空", pageNum)
					return
				}

				// 收集代理信息
				var pageProxies []*Proxy
				for _, proxyInfo := range proxyListResp.Data.Proxies {
					if proxyInfo.Status == 1 { // 只使用状态为1(有效)的代理
						// 跳过无效的IP或端口
						if proxyInfo.IP == "" || proxyInfo.Port <= 0 {
							continue
						}

						// 处理代理类型
						proxyType := strings.ToUpper(proxyInfo.Type)

						// 如果没有类型信息，则记录警告并跳过
						if proxyType == "" {
							log.Warn("跳过无类型信息的代理: %s:%d", proxyInfo.IP, proxyInfo.Port)
							continue
						}

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

						pageProxies = append(pageProxies, proxy)
					}
				}

				// 添加到总列表
				if len(pageProxies) > 0 {
					mu.Lock()
					allProxies = append(allProxies, pageProxies...)
					atomic.AddInt32(&successCount, 1)
					mu.Unlock()

					log.Info("第%d页获取到%d个有效代理", pageNum, len(pageProxies))
				} else {
					log.Warn("第%d页未找到有效代理", pageNum)
				}
			}(page)
		}

		// 等待所有请求完成
		wg.Wait()
	}

	// 检查是否至少有一个页面成功
	if atomic.LoadInt32(&successCount) == 0 {
		return nil, fmt.Errorf("所有页面获取失败，未能获得任何有效代理")
	}

	// 去重
	uniqueProxies := make(map[string]*Proxy)
	for _, proxy := range allProxies {
		uniqueProxies[proxy.Address] = proxy
	}

	// 转回切片
	allProxies = make([]*Proxy, 0, len(uniqueProxies))
	for _, proxy := range uniqueProxies {
		allProxies = append(allProxies, proxy)
	}

	if len(allProxies) == 0 {
		return nil, fmt.Errorf("未找到有效的代理")
	}

	// 记录代理统计信息
	// 这里不需要检查len > 0，因为前面已经检查过了，这里只是记录统计信息
	typeCount := make(map[string]int)
	countryCount := make(map[string]int)

	for _, p := range allProxies {
		typeCount[p.Type]++
		countryCount[p.Country]++
	}

	log.Info("共获取到 %d 个有效代理，类型分布: %v", len(allProxies), typeCount)
	log.Debug("国家/地区分布: %v", countryCount)
	
	return allProxies, nil
}

// verifyProxy 验证代理是否有效
func (m *Manager) verifyProxy(proxyAddr string, proxyType string) bool {
	// 直接使用传入的代理类型
	if proxyType == "" {
		log.Warn("代理 %s 无类型信息，无法验证", proxyAddr)
		return false
	}

	log.Debug("开始验证代理: %s (类型: %s)", proxyAddr, proxyType)

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
	log.Debug("使用HTTPS验证URL验证%s代理: %s", proxyType, proxyAddr)
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		log.Error("解析代理地址 %s 失败: %v", proxyAddr, err)
		return false
	}

	// 获取代理主机IP
	proxyHost := proxyURL.Hostname()
	log.Debug("代理主机IP: %s", proxyHost)

	// 创建一个使用代理的HTTP客户端
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   proxyTimeout,
	}

	req, err := http.NewRequest("GET", verifyURL, nil)
	if err != nil {
		log.Error("创建验证请求失败: %v", err)
		return false
	}

	// 添加随机User-Agent
	randomUA := constants.GetRandomUserAgent()
	req.Header.Set("User-Agent", randomUA)
	log.Debug("验证代理 %s 使用UA: %s", proxyAddr, randomUA)

	// 设置更长的超时时间进行验证
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

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

	// 核心验证逻辑修改：当返回的IP与代理主机IP相同时，视为代理有效
	isValid := returnedIP == proxyHost
	if isValid {
		log.Info("代理 %s 验证成功：返回IP(%s)与代理主机IP匹配", proxyAddr, returnedIP)
	} else {
		log.Warn("代理 %s 验证失败：返回IP(%s)与代理主机IP(%s)不匹配", proxyAddr, returnedIP, proxyHost)
	}
	
	return isValid
}

// ForceRefreshProxy 强制刷新代理池并返回一个新代理
func (m *Manager) ForceRefreshProxy() (string, error) {
	log.Info("正在强制刷新代理池...")

	// 启动异步刷新
	refreshDone := make(chan struct{})
	var refreshErr error
	go func() {
		defer close(refreshDone)
		refreshErr = m.refreshProxyPool()
	}()

	// 获取当前代理池中的一个代理作为临时使用
	m.mu.RLock()
	currentPoolSize := len(m.proxyPool)
	var tempProxy *Proxy
	if currentPoolSize > 0 {
		// 优先选择最近验证成功且失败次数为0的代理
		validProxies := make([]*Proxy, 0)
		for _, p := range m.proxyPool {
			if p.FailCount == 0 {
				validProxies = append(validProxies, p)
			}
		}
		
		if len(validProxies) > 0 {
			// 从有效代理中随机选择一个
			tempProxy = validProxies[rand.Intn(len(validProxies))]
		} else if currentPoolSize > 0 {
			// 如果没有验证成功的代理，从所有代理中随机选择
			tempProxy = m.proxyPool[rand.Intn(currentPoolSize)]
		}
	}
	m.mu.RUnlock()

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

	// 从刷新后的代理池中获取代理
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.proxyPool) == 0 {
		return "", fmt.Errorf("代理池为空")
	}

	// 筛选验证成功的代理
	candidates := make([]*Proxy, 0)
	for _, p := range m.proxyPool {
		if p.FailCount == 0 && !p.LastVerify.IsZero() {
			candidates = append(candidates, p)
		}
	}

	var proxy *Proxy
	if len(candidates) > 0 {
		// 按响应时间排序，优先选择响应快的代理
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].ResponseTime < candidates[j].ResponseTime
		})
		
		// 从响应时间最快的前三个中随机选择一个（如果有三个的话）
		candidateCount := len(candidates)
		if candidateCount > 3 {
			candidateCount = 3
		}
		randIndex := rand.Intn(candidateCount)
		proxy = candidates[randIndex]
	} else {
		// 如果没有满足条件的候选代理，则从整个池中随机选择
		randIndex := rand.Intn(len(m.proxyPool))
		proxy = m.proxyPool[randIndex]
	}

	// 根据代理类型添加协议前缀
	formattedAddress := formatProxyAddress(proxy.Address, proxy.Type)

	log.Info("强制刷新后获取到代理: %s (类型: %s, 响应时间: %dms)",
		formattedAddress, proxy.Type, proxy.ResponseTime)
	return formattedAddress, nil
}
