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
			// 这里依然从proxyPool中查找类型，因为这些代理已经在池中
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
			// 将代理类型信息传递给verifyProxy函数
			if m.verifyProxy(p.Address, p.Type) {
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

// 添加本地代理到代理池
func (m *Manager) addLocalProxies() {
	localProxies := []*Proxy{
		{
			Address:      "127.0.0.1:7890",
			LastVerify:   time.Now(),
			Type:         "HTTP",
			Country:      "本地",
			ResponseTime: 1,
		},
		{
			Address:      "localhost:1080",
			LastVerify:   time.Now(),
			Type:         "SOCKS5",
			Country:      "本地",
			ResponseTime: 1,
		},
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// 检查是否已存在相同地址的代理
	existingProxies := make(map[string]bool)
	for _, p := range m.proxyPool {
		existingProxies[p.Address] = true
	}
	
	// 添加不重复的本地代理
	for _, localProxy := range localProxies {
		if !existingProxies[localProxy.Address] {
			m.proxyPool = append(m.proxyPool, localProxy)
			log.Info("添加本地代理: %s (%s)", localProxy.Address, localProxy.Type)
		}
	}
}

// fetchProxiesFromAPI 从指定的API批量获取代理
func (m *Manager) fetchProxiesFromAPI() ([]*Proxy, error) {
	// 设置随机User-Agent
	randomUA := constants.GetRandomUserAgent()
	m.httpClient.SetHeader("User-Agent", randomUA)
	log.Info("获取代理使用UA: %s", randomUA)

	// 获取多页代理数据
	var allProxies []*Proxy
	
	// 初始化为maxPagesFetch，后续根据API返回的总页数动态调整
	totalPages := maxPagesFetch
	var totalPagesLock sync.Mutex
	
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

	// 并发获取多页数据
	for page := 1; page <= totalPages; page++ {
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

			// 可选：按国家/地区筛选，可以根据需要启用
			// params["country"] = "香港,日本,新加坡"

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

			// 关闭调试模式
			debug := false // 关闭调试模式
			if debug {
				debugDir := "debug"
				if _, err := os.Stat(debugDir); os.IsNotExist(err) {
					os.Mkdir(debugDir, 0755)
				}
				fileName := fmt.Sprintf("%s/proxy_resp_page%d.json", debugDir, pageNum)
				err := os.WriteFile(fileName, resp.Body(), 0644)
				if err != nil {
					log.Warn("保存响应内容失败: %v", err)
				}
			}

			// 打印原始响应以便调试
			respBody := resp.Body()
			if len(respBody) > 0 {
				sample := string(respBody)
				if len(sample) > 200 {
					sample = sample[:200] + "..." // 截断长响应
				}
				log.Debug("第%d页响应样本: %s", pageNum, sample)
			}

			var proxyListResp ProxyListResponse
			if err := json.Unmarshal(respBody, &proxyListResp); err != nil {
				log.Warn("解析第%d页响应失败: %v", pageNum, err)
				// 尝试解析为通用JSON，获取更多错误信息
				var genericResp map[string]interface{}
				if jsonErr := json.Unmarshal(respBody, &genericResp); jsonErr == nil {
					log.Warn("第%d页响应通用解析: %+v", pageNum, genericResp)
				}
				return
			}

			// 更全面的响应验证
			if !proxyListResp.Success {
				log.Warn("第%d页返回success=false", pageNum)
				return
			}

			// 检查并记录API返回的分页信息
			if proxyListResp.Data.Pagination.CurrentPage > 0 {
				log.Info("第%d页分页信息: 当前页=%d, 每页=%d, 总页=%d, 总数=%d",
					pageNum,
					proxyListResp.Data.Pagination.CurrentPage,
					proxyListResp.Data.Pagination.PerPage,
					proxyListResp.Data.Pagination.TotalPages,
					proxyListResp.Data.Pagination.Total)
				
				// 更新总页数，确保爬取所有页面
				if proxyListResp.Data.Pagination.TotalPages > 0 {
					totalPagesLock.Lock()
					if proxyListResp.Data.Pagination.TotalPages < totalPages {
						totalPages = proxyListResp.Data.Pagination.TotalPages
					}
					totalPagesLock.Unlock()
					
					// 如果当前只启动了有限数量的爬取协程，但API返回了更多页面，则继续启动协程爬取剩余页面
					totalPagesLock.Lock()
					currentTotalPages := totalPages
					totalPagesLock.Unlock()
					
					if pageNum == 1 && currentTotalPages > 10 {
						// 启动更多协程爬取剩余页面
						for additionalPage := 11; additionalPage <= currentTotalPages; additionalPage++ {
							wg.Add(1)
							go func(p int) {
								defer wg.Done()
								
								// 请求前获取限速令牌
								log.Debug("等待获取额外页面%d的限速令牌", p)
								<-rateLimiter
								log.Debug("获得额外页面%d的限速令牌，开始请求", p)
								
								// 为额外页面创建一个新的请求
								additionalParams := map[string]string{
									"page":     fmt.Sprintf("%d", p),
									"per_page": "100",
									"type":     "socks5",
								}
								
								// 发送请求获取额外页面
								additionalUA := constants.GetRandomUserAgent()
								additionalResp, err := m.httpClient.R().
									SetQueryParams(additionalParams).
									SetHeader("User-Agent", additionalUA).
									Get(proxyAPIBaseURL)
									
								if err != nil || additionalResp.StatusCode() != http.StatusOK {
									log.Warn("请求额外页面%d失败", p)
									return
								}
								
								// 解析响应
								var additionalProxyListResp ProxyListResponse
								if err := json.Unmarshal(additionalResp.Body(), &additionalProxyListResp); err != nil {
									log.Warn("解析额外页面%d响应失败: %v", p, err)
									return
								}
								
								if !additionalProxyListResp.Success || additionalProxyListResp.Data.Proxies == nil {
									return
								}
								
								// 处理代理数据
								var pageProxies []*Proxy
								for _, proxyInfo := range additionalProxyListResp.Data.Proxies {
									if proxyInfo.Status == 1 && proxyInfo.IP != "" && proxyInfo.Port > 0 {
										proxyType := strings.ToUpper(proxyInfo.Type)
										if proxyType == ProxyTypeSOCKS5 {
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
								}
								
								// 添加到总列表
								if len(pageProxies) > 0 {
									mu.Lock()
									allProxies = append(allProxies, pageProxies...)
									atomic.AddInt32(&successCount, 1)
									mu.Unlock()
									
									log.Info("额外页面%d获取到%d个有效代理", p, len(pageProxies))
								}
							}(additionalPage)
						}
					}
				}
			}

			// 检查代理数据
			if proxyListResp.Data.Proxies == nil {
				log.Warn("第%d页代理数据为null", pageNum)
				return
			}

			if len(proxyListResp.Data.Proxies) == 0 {
				log.Warn("第%d页代理数据为空数组", pageNum)
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

					// 记录具体的代理详情，以便于调试
					log.Debug("处理代理: ID=%d, IP=%s, 端口=%d, 类型=%s, 国家=%s, 响应时间=%dms",
						proxyInfo.ID, proxyInfo.IP, proxyInfo.Port, proxyType,
						proxyInfo.Country, proxyInfo.ResponseTime)

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

				log.Info("第%d页获取到%d个有效代理，样例: %s (%s)",
					pageNum, len(pageProxies),
					pageProxies[0].Address, pageProxies[0].Type)
			} else {
				log.Warn("第%d页未找到有效代理", pageNum)
			}

		}(page)
	}

	// 等待所有请求完成
	wg.Wait()

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

	if len(allProxies) > 0 {
		// 记录具体的代理类型统计
		typeCount := make(map[string]int)
		countryCount := make(map[string]int)

		for _, p := range allProxies {
			typeCount[p.Type]++
			countryCount[p.Country]++
		}

		log.Info("共获取到 %d 个有效代理，类型分布: %v", len(allProxies), typeCount)
		log.Debug("国家/地区分布: %v", countryCount)
	} else {
		log.Warn("没有获取到任何有效代理")
	}
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
