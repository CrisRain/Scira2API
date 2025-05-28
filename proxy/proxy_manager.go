package proxy

import (
	"context"
	"crypto/tls" // Keep for InsecureSkipVerify, or remove if TLS config is removed/changed
	"encoding/json"
	"net" // Added for net.Conn for SOCKS5 dialer
	"golang.org/x/net/proxy" // Added for SOCKS5 proxy
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
	"sync/atomic" // Added for atomic counters
	"time"

	"scira2api/log"
	"scira2api/pkg/constants"
)

const (
	// 代理API基础URL
	proxyAPIBaseURL = "https://proxy.scdn.io/api/proxy_list.php"
	// 验证URL (只使用HTTPS版本)
	proxyVerifyURL = "https://ip.gs/"
	proxyTimeout   = 10 * time.Second
	maxRetries     = 3
	retryDelay     = 2 * time.Second

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
	httpClient   *http.Client
	proxyPool    []*Proxy     // 代理池
	mu           sync.RWMutex // 保护代理池的互斥锁
	ctx          context.Context
	cancel       context.CancelFunc
	refreshWg    sync.WaitGroup // 用于等待后台任务完成
	poolFilePath string         // 代理池持久化文件路径
}

// NewManager 创建一个新的代理管理器实例并启动代理池维护
func NewManager() *Manager {
	// 创建标准库HTTP客户端
	client := &http.Client{
		Timeout: proxyTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

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
		// 如果fetchAndVerifyProxies成功执行（可能添加了新代理并已保存），
		// 这里的保存是为了确保在!loaded为true，但API未返回任何新代理时，
		// 至少能保存一次空的或仅包含本地代理的池。
		// 但如果fetchAndVerifyProxies内部已经保存，这里的保存可能是冗余的。
		// 考虑到fetchAndVerifyProxies仅在*新添加*时保存，这里的保存仍然有意义，
		// 确保在没有新代理添加但池被清空等情况下，状态能被持久化。
		// 或者，如果API调用成功但未添加任何新代理，也应保存当前池状态。
		log.Info("API代理获取流程完成，检查是否需要保存当前代理池状态...")
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
	case ProxyTypeHTTP:
		// HTTP代理使用http://前缀
		return "http://" + address
	case ProxyTypeHTTPS:
		// HTTPS代理使用https://前缀
		return "https://" + address
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

	// fetchAndVerifyProxies 将直接修改 m.proxyPool 并按需保存
	err := m.fetchAndVerifyProxies()
	if err != nil {
		log.Error("刷新代理池时，获取新代理失败: %v", err)
		return err // 返回错误，但不一定意味着池子是空的或不可用
	}

	log.Info("代理池刷新流程执行完毕")
	// 无需在此处显式保存，fetchAndVerifyProxies 会在添加新代理时保存
	// 如果 fetchAndVerifyProxies 未添加任何新代理，则池状态未变，无需重复保存

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

	// 串行验证代理池中的代理，并直接更新
	var validatedCount int
	var removedCount int

	for _, proxyInSnapshot := range proxiesToValidate {
		isStillInPoolAndUpdated := false // 标记代理是否仍在池中并且其状态被更新了

		// 验证前先获取当前池中对应代理的最新失败次数
		// 这一步是为了确保FailCount的增加是基于最新的状态，尽管验证本身用的是快照信息
		currentFailCountInMainPool := 0
		m.mu.RLock()
		foundInMainPoolForFailCount := false
		for _, pMain := range m.proxyPool {
			if pMain.Address == proxyInSnapshot.Address {
				currentFailCountInMainPool = pMain.FailCount
				foundInMainPoolForFailCount = true
				break
			}
		}
		m.mu.RUnlock()

		if !foundInMainPoolForFailCount {
			log.Debug("代理 %s 在验证前已从池中移除(获取FailCount时未找到)，跳过验证", proxyInSnapshot.Address)
			continue
		}
		
		if m.verifyProxy(proxyInSnapshot.Address, proxyInSnapshot.Type) {
			validatedCount++
			m.mu.Lock()
			// 遍历主代理池，更新验证成功的代理
			for i, p := range m.proxyPool {
				if p.Address == proxyInSnapshot.Address {
					m.proxyPool[i].LastVerify = time.Now()
					m.proxyPool[i].FailCount = 0
					isStillInPoolAndUpdated = true
					log.Debug("代理 %s 重新验证成功", p.Address)
					break
				}
			}
			m.mu.Unlock()
		} else {
			m.mu.Lock()
			// 遍历主代理池，处理验证失败的代理
			tempPool := make([]*Proxy, 0, len(m.proxyPool))
			foundAndProcessed := false
			for _, p := range m.proxyPool {
				if p.Address == proxyInSnapshot.Address && !foundAndProcessed {
					foundAndProcessed = true
					p.FailCount = currentFailCountInMainPool + 1 // 使用验证前获取的FailCount来递增
					isStillInPoolAndUpdated = true // 标记状态已更新（即使是增加失败次数）
					if p.FailCount >= 3 {
						removedCount++
						log.Warn("代理 %s 连续验证失败 %d 次，从池中移除", p.Address, p.FailCount)
						// 不将此代理添加到tempPool以实现移除
					} else {
						log.Debug("代理 %s 验证失败，失败次数增加到 %d", p.Address, p.FailCount)
						tempPool = append(tempPool, p) // 保留，但失败次数已更新
					}
				} else {
					tempPool = append(tempPool, p)
				}
			}
			m.proxyPool = tempPool
			m.mu.Unlock()
		}

		// 如果代理仍在池中并且其状态（LastVerify, FailCount）被更新，或者被移除了，则保存
		if isStillInPoolAndUpdated {
			m.saveProxyPoolToFile()
		}
	}

	m.mu.RLock()
	finalPoolSize := len(m.proxyPool)
	m.mu.RUnlock()
	log.Info("代理池验证周期完成。重新验证成功: %d, 移除: %d, 当前池大小: %d", validatedCount, removedCount, finalPoolSize)
	// 注意：这里的saveProxyPoolToFile()被移除了，因为保存操作在循环内部按需执行。
}

// fetchAndVerifyProxies 从API获取代理，验证后直接添加到池中并保存
func (m *Manager) fetchAndVerifyProxies() error {
	// 获取代理列表
	apiProxies, err := m.fetchProxiesFromAPI() // Renamed proxyList to apiProxies for clarity
	if err != nil {
		return err // Error includes "未能获得任何有效代理" or "所有页面获取失败" from fetchProxiesFromAPI
	}

	if len(apiProxies) == 0 {
		log.Info("API未返回任何代理进行验证")
		return nil // Not an error, just no proxies to process
	}

	log.Info("从API获取到 %d 个代理，开始验证并按需添加到池中...", len(apiProxies))

	maxConcurrent := 20 // 最大并发验证数量
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

			if m.verifyProxy(p.Address, p.Type) {
				p.LastVerify = time.Now()
				p.FailCount = 0

				m.mu.Lock()
				isExisting := false
				for _, existingProxy := range m.proxyPool {
					if existingProxy.Address == p.Address {
						isExisting = true
						// Optionally update existing proxy's metadata if verifyProxy was for an existing one,
						// but here p is a new object from API.
						// For now, we only add if it's truly new.
						// existingProxy.LastVerify = p.LastVerify // Example if update was desired
						// existingProxy.ResponseTime = p.ResponseTime
						break
					}
				}

				if !isExisting {
					m.proxyPool = append(m.proxyPool, p)
					log.Info("新代理验证成功并已添加: %s (类型: %s, 响应时间: %dms)", p.Address, p.Type, p.ResponseTime)
					// atomic.AddInt32(&addedCount, 1) // Moved save outside lock, count before unlock
				} else {
					log.Debug("已验证代理 %s 已存在于池中，未重复添加", p.Address)
				}
				m.mu.Unlock() // Release lock before potentially saving

				if !isExisting { // Only save if a new proxy was actually added
					atomic.AddInt32(&addedCount, 1) // Increment count here before saving
					m.saveProxyPoolToFile() // Persist immediately after adding a new proxy
				}
			} else {
				log.Debug("API提供的代理验证失败: %s (类型: %s)", p.Address, p.Type)
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

// 添加本地代理到代理池 - 当前已禁用预定义本地代理
func (m *Manager) addLocalProxies() {
	// 本地代理已禁用
	log.Info("本地代理功能已禁用")
}

// fetchProxiesFromAPI 从指定的API批量获取代理
func (m *Manager) fetchProxiesFromAPI() ([]*Proxy, error) {
	// 设置随机User-Agent (将在请求中单独设置)
	randomUA := constants.GetRandomUserAgent()
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

	var successCount int // 成功获取的页数

	// 先获取第一页以确定总页数
	log.Info("先获取第一页代理以确定总页数...")

	// 获取限速令牌
	<-rateLimiter

	// 发送请求获取第一页
	currUA := constants.GetRandomUserAgent()
	apiURL := proxyAPIBaseURL + "?page=1&per_page=100&type=socks5" // SOCKS5 or relevant type
	
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Warn("创建第1页代理请求失败: %v", err)
		return nil, err
	}
	req.Header.Set("User-Agent", currUA)

	// 决定使用哪个HTTP客户端 (带代理或不带代理)
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
				tempTransport := &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 与verifyProxy一致
				}
				// 根据代理类型配置Transport (与verifyProxy逻辑类似)
				switch strings.ToLower(parsedProxyURL.Scheme) {
				case "http", "https":
					tempTransport.Proxy = http.ProxyURL(parsedProxyURL)
				case "socks5":
					dialer, err_socks := proxy.SOCKS5("tcp", parsedProxyURL.Host, nil, proxy.Direct)
					if err_socks == nil {
						tempTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
							return dialer.Dial(network, addr)
						}
					} else {
						log.Warn("为API请求创建SOCKS5 dialer失败 (%s): %v, 将不使用此代理", proxyAddrToUse, err_socks)
						// 回退到默认client
					}
				default:
					log.Warn("从池中获取的代理 %s 类型 %s 不支持用于爬取API, 将不使用此代理", proxyAddrToUse, parsedProxyURL.Scheme)
					// 回退到默认client
				}
				// 只有在成功配置代理时才创建新client
				if tempTransport.Proxy != nil || tempTransport.DialContext != nil {
					clientToUse = &http.Client{
						Transport: tempTransport,
						Timeout:   m.httpClient.Timeout, // 使用与m.httpClient相同的超时
					}
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

	// 发送请求
	resp, err := clientToUse.Do(req)
	if err != nil {
		log.Warn("请求第1页代理失败: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn("第1页返回错误状态码: %d", resp.StatusCode)
		return nil, fmt.Errorf("API返回错误状态码: %d", resp.StatusCode)
	}

	// 解析第一页响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warn("读取第1页响应失败: %v", err)
		return nil, err
	}

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
	allProxies = append(allProxies, firstPageProxies...)
	if len(firstPageProxies) > 0 {
		successCount++
	}

	log.Info("第1页获取到%d个有效代理", len(firstPageProxies))

	// 如果有多页，串行爬取剩余页面（从第2页开始）
	if totalPages > 1 {
		for pageNum := 2; pageNum <= totalPages; pageNum++ {
			// 请求前获取限速令牌
			log.Debug("等待获取页面%d的限速令牌", pageNum)
			<-rateLimiter
			log.Debug("获得页面%d的限速令牌，开始请求", pageNum)

			// 发送请求，每个请求使用新的随机UA
			currUA := constants.GetRandomUserAgent()

			// 构建URL和请求
			pageAPIURL := fmt.Sprintf("%s?page=%d&per_page=100&type=socks5", proxyAPIBaseURL, pageNum)
			req, err := http.NewRequest("GET", pageAPIURL, nil)
			if err != nil {
				log.Warn("创建第%d页代理请求失败: %v", pageNum, err)
				continue
			}
			req.Header.Set("User-Agent", currUA)

			// 决定使用哪个HTTP客户端 (与第一页逻辑相同)
			clientToUseForPage := m.httpClient
			
			m.mu.RLock()
			pagePoolNotEmpty := len(m.proxyPool) > 0
			m.mu.RUnlock()

			if pagePoolNotEmpty {
				proxyAddrToUsePage, err_get_proxy_page := m.GetProxy()
				if err_get_proxy_page == nil && proxyAddrToUsePage != "" {
					parsedProxyURLPage, err_parse_proxy_page := url.Parse(proxyAddrToUsePage)
					if err_parse_proxy_page == nil {
						log.Info("尝试使用池中代理 %s 爬取API %s (第%d页)", proxyAddrToUsePage, pageAPIURL, pageNum)
						tempTransportPage := &http.Transport{
							TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
						}
						switch strings.ToLower(parsedProxyURLPage.Scheme) {
						case "http", "https":
							tempTransportPage.Proxy = http.ProxyURL(parsedProxyURLPage)
						case "socks5":
							dialerPage, err_socks_page := proxy.SOCKS5("tcp", parsedProxyURLPage.Host, nil, proxy.Direct)
							if err_socks_page == nil {
								tempTransportPage.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
									return dialerPage.Dial(network, addr)
								}
							} else {
								log.Warn("为API请求(第%d页)创建SOCKS5 dialer失败 (%s): %v, 不使用此代理", pageNum, proxyAddrToUsePage, err_socks_page)
							}
						default:
							log.Warn("从池中获取的代理 %s 类型 %s (第%d页)不支持用于爬取API, 不使用此代理", proxyAddrToUsePage, parsedProxyURLPage.Scheme, pageNum)
						}
						if tempTransportPage.Proxy != nil || tempTransportPage.DialContext != nil {
							clientToUseForPage = &http.Client{
								Transport: tempTransportPage,
								Timeout:   m.httpClient.Timeout,
							}
						}
					} else {
						log.Warn("解析从池中获取的代理地址 %s (第%d页)失败: %v, 不使用此代理", proxyAddrToUsePage, pageNum, err_parse_proxy_page)
					}
				} else if err_get_proxy_page != nil {
					log.Warn("从池中获取代理(第%d页)失败: %v, 不使用代理爬取API", pageNum, err_get_proxy_page)
				}
			} else {
				log.Info("代理池为空，直接爬取API %s (第%d页)", pageAPIURL, pageNum)
			}
			
			// 发送请求
			resp, err := clientToUseForPage.Do(req)
			if err != nil {
				log.Warn("请求第%d页代理失败: %v", pageNum, err)
				continue // 继续尝试下一页
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				log.Warn("第%d页返回错误状态码: %d", pageNum, resp.StatusCode)
				continue // 继续尝试下一页
			}

			// 解析响应
			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Warn("读取第%d页响应失败: %v", pageNum, err)
				continue // 继续尝试下一页
			}

			var proxyListResp ProxyListResponse
			if err := json.Unmarshal(respBody, &proxyListResp); err != nil {
				log.Warn("解析第%d页响应失败: %v", pageNum, err)
				continue // 继续尝试下一页
			}

			// 验证响应
			if !proxyListResp.Success {
				log.Warn("第%d页返回success=false", pageNum)
				continue // 继续尝试下一页
			}

			// 检查代理数据
			if len(proxyListResp.Data.Proxies) == 0 {
				log.Warn("第%d页代理数据为空", pageNum)
				continue // 继续尝试下一页
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
				allProxies = append(allProxies, pageProxies...)
				successCount++
				log.Info("第%d页获取到%d个有效代理", pageNum, len(pageProxies))
			} else {
				log.Warn("第%d页未找到有效代理", pageNum)
			}
		}
	}

	// 检查是否至少有一个页面成功
	if successCount == 0 {
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
		// TLSClientConfig is kept as per original logic.
		// If InsecureSkipVerify is not desired for all proxy types,
		// this might need further conditional logic.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// Configure proxy based on scheme
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DialContext = nil // Ensure SOCKS dialer (if any) is not used
	case "socks5":
		// Assuming no authentication for SOCKS5 proxy for now (auth = nil)
		dialer, err_socks := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err_socks != nil {
			log.Error("为SOCKS5代理 %s 创建dialer失败: %v", proxyAddr, err_socks)
			return false
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
		transport.Proxy = nil // Ensure HTTP proxy (if any) is not used
	// case "socks4": // SOCKS4 is a defined ProxyType but not handled here or in formatProxyAddress for scheme
	// log.Warn("SOCKS4代理验证尚不支持: %s", proxyAddr)
	// return false
	default:
		log.Warn("不支持的代理协议 %s 用于验证: %s", proxyURL.Scheme, proxyAddr)
		return false
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	// The original code returned true here, which seems incorrect if isValid is false.
	// Assuming the intent was to return the actual validation status.
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
