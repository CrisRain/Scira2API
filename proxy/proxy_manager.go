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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"scira2api/log"
	"scira2api/pkg/constants"
)

const (
	// 代理API基础URL
	proxyAPIBaseURL  = "https://proxy.scdn.io/api/proxy_list.php"
	proxyVerifyURL   = "https://ip.gs/"
	proxyTimeout     = 10 * time.Second
	maxRetries       = 3
	retryDelay       = 2 * time.Second
	
	// 代理池相关常量
	minPoolSize      = 20      // 代理池最小数量，低于此值时触发刷新
	refreshInterval  = 5 * time.Minute  // 定时刷新间隔
	validateInterval = 2 * time.Minute  // 代理验证间隔
	
	// 代理池持久化
	proxyPoolFile    = "pool/proxy_pool.json" // 代理池持久化文件
	maxPagesFetch    = 10                // 从API获取的最大页数
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
			CurrentPage    int `json:"current_page"`
			PerPage        int `json:"per_page"`
			TotalPages     int `json:"total_pages"`
			TotalFiltered  int `json:"total_filtered"`
			TotalActive    int `json:"total_active"`
			Total          int `json:"total"`
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
				m.validateProxies()
			}
		}
	}()
	
	log.Info("代理池维护任务已启动")
}

// loadProxyPoolFromFile 从文件加载代理池
func (m *Manager) loadProxyPoolFromFile() bool {
	log.Info("尝试从文件加载代理池: %s", m.poolFilePath)
	
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
	
	// 过滤掉过期的代理（超过24小时未验证的）
	var validProxies []*Proxy
	now := time.Now()
	for _, proxy := range proxies {
		if now.Sub(proxy.LastVerify) < 24*time.Hour {
			validProxies = append(validProxies, proxy)
		}
	}
	
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
		return "http://" + address
	case ProxyTypeSOCKS4:
		return "socks4://" + address
	case ProxyTypeSOCKS5:
		return "socks5://" + address
	default:
		return "socks5://" + address
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
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// 合并新旧代理池，保留不重复的代理
	existingProxies := make(map[string]bool)
	for _, p := range m.proxyPool {
		existingProxies[p.Address] = true
	}
	
	// 添加新的不重复代理
	for _, newProxy := range newProxies {
		if !existingProxies[newProxy.Address] {
			m.proxyPool = append(m.proxyPool, newProxy)
		}
	}
	
	log.Info("代理池刷新完成，当前代理数量: %d", len(m.proxyPool))
	
	// 保存更新后的代理池到文件
	go m.saveProxyPoolToFile()
	
	return nil
}

// validateProxies 验证代理池中的代理
func (m *Manager) validateProxies() {
	log.Info("开始验证代理池中的代理...")
	
	m.mu.RLock()
	proxiesToValidate := make([]*Proxy, len(m.proxyPool))
	copy(proxiesToValidate, m.proxyPool)
	m.mu.RUnlock()
	
	var validProxies []*Proxy
	var wg sync.WaitGroup
	var mu sync.Mutex // 保护validProxies
	
	// 并发验证代理
	for _, proxy := range proxiesToValidate {
		wg.Add(1)
		go func(p *Proxy) {
			defer wg.Done()
			if m.verifyProxy(p.Address) {
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
	poolSize := len(m.proxyPool)
	m.mu.Unlock()
	
	log.Info("代理验证完成，有效代理数量: %d", poolSize)
	
	// 如果代理池数量低于最小值，触发刷新
	if poolSize < minPoolSize {
		log.Info("代理池数量 (%d) 低于最小值 (%d)，触发刷新", poolSize, minPoolSize)
		go m.refreshProxyPool()
	} else {
		// 保存更新后的代理池到文件
		go m.saveProxyPoolToFile()
	}
}

// fetchAndVerifyProxies 批量获取并验证代理
func (m *Manager) fetchAndVerifyProxies() ([]*Proxy, error) {
	// 获取代理列表
	proxyList, err := m.fetchProxiesFromAPI()
	if err != nil {
		return nil, err
	}
	
	log.Info("从API获取到 %d 个代理，开始验证...", len(proxyList))
	
	var validProxies []*Proxy
	var wg sync.WaitGroup
	var mu sync.Mutex // 保护validProxies
	
	// 并发验证代理
	for _, proxy := range proxyList {
		wg.Add(1)
		go func(p *Proxy) {
			defer wg.Done()
			if m.verifyProxy(p.Address) {
				mu.Lock()
				validProxies = append(validProxies, p)
				mu.Unlock()
			}
		}(proxy)
	}
	
	wg.Wait()
	
	// 按响应时间排序，优先使用响应快的代理
	sort.Slice(validProxies, func(i, j int) bool {
		return validProxies[i].ResponseTime < validProxies[j].ResponseTime
	})
	
	log.Info("代理验证完成，有效代理数量: %d/%d", len(validProxies), len(proxyList))
	return validProxies, nil
}

// fetchProxiesFromAPI 从指定的API批量获取代理
func (m *Manager) fetchProxiesFromAPI() ([]*Proxy, error) {
	// 设置随机User-Agent
	randomUA := constants.GetRandomUserAgent()
	m.httpClient.SetHeader("User-Agent", randomUA)
	log.Info("获取代理使用UA: %s", randomUA)
	
	// 获取多页代理数据
	var allProxies []*Proxy
	totalPages := maxPagesFetch
	
	var wg sync.WaitGroup
	var mu sync.Mutex // 保护allProxies
	var fetchErrors []error
	
	// 并发获取多页数据
	for page := 1; page <= totalPages; page++ {
		wg.Add(1)
		go func(pageNum int) {
			defer wg.Done()
			
			// 构建请求URL及参数
			params := map[string]string{
				"page":     fmt.Sprintf("%d", pageNum),
				"per_page": "100",
			}
			
			// 可以根据需求添加过滤条件
			// 例如: 同时获取多种类型的代理
			params["type"] = "HTTP,HTTPS,SOCKS5"
			// 例如: 按国家/地区筛选
			// params["country"] = "香港,日本,新加坡"
			
			// 发送请求
			resp, err := m.httpClient.R().
				SetQueryParams(params).
				SetHeader("User-Agent", constants.GetRandomUserAgent()). // 每个请求使用不同的UA
				Get(proxyAPIBaseURL)
			
			if err != nil {
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Errorf("请求第%d页代理失败: %w", pageNum, err))
				mu.Unlock()
				return
			}
			
			if resp.StatusCode() != http.StatusOK {
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Errorf("第%d页返回错误状态码: %d", pageNum, resp.StatusCode()))
				mu.Unlock()
				return
			}
			
			var proxyListResp ProxyListResponse
			if err := json.Unmarshal(resp.Body(), &proxyListResp); err != nil {
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Errorf("解析第%d页响应失败: %w", pageNum, err))
				mu.Unlock()
				return
			}
			
			if !proxyListResp.Success || len(proxyListResp.Data.Proxies) == 0 {
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Errorf("第%d页返回无效数据", pageNum))
				mu.Unlock()
				return
			}
			
			// 收集代理信息
			var pageProxies []*Proxy
			for _, proxyInfo := range proxyListResp.Data.Proxies {
				if proxyInfo.Status == 1 { // 只使用状态为1(有效)的代理
					proxy := &Proxy{
						Address:      fmt.Sprintf("%s:%d", proxyInfo.IP, proxyInfo.Port),
						LastVerify:   time.Now(),
						Type:         proxyInfo.Type,
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
				mu.Unlock()
				
				log.Info("第%d页获取到%d个有效代理", pageNum, len(pageProxies))
			}
			
		}(page)
	}
	
	// 等待所有请求完成
	wg.Wait()
	
	// 检查是否所有页面都失败了
	if len(fetchErrors) == totalPages {
		return nil, fmt.Errorf("所有页面获取失败: %v", fetchErrors[0])
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
	
	log.Info("共获取到 %d 个有效代理", len(allProxies))
	return allProxies, nil
}

// verifyProxy 验证SOCKS5代理是否有效
func (m *Manager) verifyProxy(proxyAddr string) bool {
	// 查找代理类型
	var proxyType string
	m.mu.RLock()
	for _, p := range m.proxyPool {
		if p.Address == proxyAddr {
			proxyType = p.Type
			break
		}
	}
	m.mu.RUnlock()
	
	// 默认使用SOCKS5
	if proxyType == "" {
		proxyType = ProxyTypeSOCKS5
	}
	
	// 确保代理地址有正确的协议前缀
	if !strings.Contains(proxyAddr, "://") {
		switch proxyType {
		case ProxyTypeHTTP, ProxyTypeHTTPS:
			proxyAddr = "http://" + proxyAddr
		case ProxyTypeSOCKS4:
			proxyAddr = "socks4://" + proxyAddr
		case ProxyTypeSOCKS5:
			proxyAddr = "socks5://" + proxyAddr
		default:
			proxyAddr = "socks5://" + proxyAddr
		}
	}
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		log.Error("解析代理地址 %s 失败: %v", proxyAddr, err)
		return false
	}

	// 创建一个使用SOCKS5代理的HTTP客户端
	// 注意：resty的SetProxy方法期望的是 "http://proxy_ip:proxy_port" 或 "socks5://proxy_ip:proxy_port"
	// 但标准库的http.Transport需要一个函数来返回代理URL
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   proxyTimeout,
	}

	req, err := http.NewRequest("GET", proxyVerifyURL, nil)
	if err != nil {
		log.Error("创建验证请求失败: %v", err)
		return false
	}
	
	// 添加随机User-Agent
	randomUA := constants.GetRandomUserAgent()
	req.Header.Set("User-Agent", randomUA)
	log.Debug("验证代理 %s 使用UA: %s", proxyAddr, randomUA)

	resp, err := client.Do(req)
	if err != nil {
		log.Warn("通过代理 %s 验证IP失败: %v", proxyAddr, err)
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

	ip := strings.TrimSpace(string(body))
	// 验证返回的IP是否与代理IP的地址部分匹配 (不含端口)
	// 这是一个基本验证，因为代理服务器本身可能有多层NAT
	// 更可靠的验证是检查IP是否与本机公网IP不同
	proxyHost := proxyURL.Hostname()
	if ip == "" {
		log.Warn("通过代理 %s 验证IP返回空", proxyAddr)
		return false
	}

	log.Info("通过代理 %s 验证IP成功，返回IP: %s (代理主机: %s)", proxyAddr, ip, proxyHost)
	// 这里可以添加更复杂的验证逻辑，例如检查返回的IP是否与代理服务器的IP地址段匹配，
	// 或者与本机公网IP不同。目前简单认为能成功请求并获得IP即为有效。
	return true
}

// ForceRefreshProxy 强制刷新代理池并返回一个新代理
func (m *Manager) ForceRefreshProxy() (string, error) {
	log.Info("正在强制刷新代理池...")
	
	if err := m.refreshProxyPool(); err != nil {
		return "", fmt.Errorf("强制刷新代理池失败: %w", err)
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
	
	log.Info("强制刷新后获取到代理: %s (类型: %s)", formattedAddress, proxyType)
	return formattedAddress, nil
}