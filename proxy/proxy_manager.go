package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"scira2api/log"
	"scira2api/pkg/constants"
)

const (
	proxyAPIURL      = "https://proxy.scdn.io/api/proxy_list.php?page=1&per_page=100&type=&country="
	proxyVerifyURL   = "https://ip.gs/"
	proxyTimeout     = 10 * time.Second
	maxRetries       = 3
	retryDelay       = 2 * time.Second
	
	// 代理池相关常量
	minPoolSize      = 20      // 代理池最小数量，低于此值时触发刷新
	refreshInterval  = 5 * time.Minute  // 定时刷新间隔
	validateInterval = 2 * time.Minute  // 代理验证间隔
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
	} `json:"data"`
}

// Proxy 代表一个代理及其元数据
type Proxy struct {
	Address    string    // 代理地址，格式: "ip:port"
	LastVerify time.Time // 上次验证时间
	FailCount  int       // 连续失败次数
}

// Manager 负责管理SOCKS5代理池
type Manager struct {
	httpClient   *resty.Client
	proxyPool    []*Proxy     // 代理池
	mu           sync.RWMutex // 保护代理池的互斥锁
	ctx          context.Context
	cancel       context.CancelFunc
	refreshWg    sync.WaitGroup // 用于等待后台任务完成
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
		httpClient: client,
		ctx:        ctx,
		cancel:     cancel,
	}
	
	// 初始化代理池
	manager.initProxyPool()
	
	// 启动代理池维护任务
	manager.startPoolMaintenance()
	
	return manager
}

// initProxyPool 初始化代理池
func (m *Manager) initProxyPool() {
	log.Info("初始化代理池...")
	proxies, err := m.fetchAndVerifyProxies()
	if err != nil {
		log.Error("初始化代理池失败: %v", err)
		return
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.proxyPool = proxies
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

// Stop 停止代理池维护任务
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
		m.refreshWg.Wait()
		log.Info("代理池维护任务已停止")
	}
}

// GetProxy 从代理池中获取一个可用的SOCKS5代理地址
func (m *Manager) GetProxy() (string, error) {
	m.mu.RLock()
	
	if len(m.proxyPool) == 0 {
		m.mu.RUnlock()
		return m.refreshAndGetProxy()
	}
	
	// 随机选择一个代理
	randIndex := rand.Intn(len(m.proxyPool))
	proxy := m.proxyPool[randIndex].Address
	m.mu.RUnlock()
	
	log.Info("从代理池获取代理: %s", proxy)
	return proxy, nil
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
	proxy := m.proxyPool[randIndex].Address
	
	log.Info("从刷新后的代理池获取代理: %s", proxy)
	return proxy, nil
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
	for _, proxyAddr := range proxyList {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			if m.verifyProxy(addr) {
				mu.Lock()
				validProxies = append(validProxies, &Proxy{
					Address:    addr,
					LastVerify: time.Now(),
				})
				mu.Unlock()
			}
		}(proxyAddr)
	}
	
	wg.Wait()
	
	log.Info("代理验证完成，有效代理数量: %d/%d", len(validProxies), len(proxyList))
	return validProxies, nil
}

// fetchProxiesFromAPI 从指定的API批量获取代理
func (m *Manager) fetchProxiesFromAPI() ([]string, error) {
	// 设置随机User-Agent
	m.httpClient.SetHeader("User-Agent", constants.GetRandomUserAgent())
	
	resp, err := m.httpClient.R().Get(proxyAPIURL)
	if err != nil {
		return nil, fmt.Errorf("请求代理API失败: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("代理API返回错误状态码: %d, body: %s", resp.StatusCode(), resp.String())
	}

	var proxyListResp ProxyListResponse
	if err := json.Unmarshal(resp.Body(), &proxyListResp); err != nil {
		return nil, fmt.Errorf("解析代理API响应失败: %w, body: %s", err, resp.String())
	}

	if !proxyListResp.Success || len(proxyListResp.Data.Proxies) == 0 {
		return nil, fmt.Errorf("代理API返回无效数据")
	}

	// 将代理信息转换为 "IP:端口" 格式
	var proxyList []string
	for _, proxy := range proxyListResp.Data.Proxies {
		if proxy.Status == 1 { // 只使用状态为1(有效)的代理
			proxyAddr := fmt.Sprintf("%s:%d", proxy.IP, proxy.Port)
			proxyList = append(proxyList, proxyAddr)
		}
	}

	if len(proxyList) == 0 {
		return nil, fmt.Errorf("未找到有效的代理")
	}

	return proxyList, nil
}

// verifyProxy 验证SOCKS5代理是否有效
func (m *Manager) verifyProxy(proxyAddr string) bool {
	if !strings.HasPrefix(proxyAddr, "socks5://") {
		proxyAddr = "socks5://" + proxyAddr
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
	req.Header.Set("User-Agent", constants.GetRandomUserAgent()) // 添加随机User-Agent

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
	proxy := m.proxyPool[randIndex].Address
	
	log.Info("强制刷新后获取到代理: %s", proxy)
	return proxy, nil
}