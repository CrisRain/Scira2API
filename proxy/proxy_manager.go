package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"scira2api/log"
)

const (
	proxyAPIURL      = "https://proxy.scdn.io/api/get_proxy.php?protocol=socks5&count=1"
	proxyVerifyURL   = "https://ip.gs/"
	proxyTimeout     = 10 * time.Second
	maxRetries       = 3
	retryDelay       = 2 * time.Second
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
)

// ProxyInfo API响应结构
type ProxyInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Proxies []string `json:"proxies"`
		Count   int      `json:"count"`
	} `json:"data"`
}

// Manager 负责管理SOCKS5代理
type Manager struct {
	httpClient    *resty.Client
	currentProxy  string
	mu            sync.RWMutex
	lastFetchTime time.Time
	fetchInterval time.Duration // 代理刷新间隔
}

// NewManager 创建一个新的代理管理器实例
func NewManager(fetchInterval time.Duration) *Manager {
	client := resty.New().
		SetTimeout(proxyTimeout).
		SetRetryCount(maxRetries).
		SetRetryWaitTime(retryDelay).
		SetRetryMaxWaitTime(20*time.Second).
		SetHeader("User-Agent", defaultUserAgent)

	return &Manager{
		httpClient:    client,
		fetchInterval: fetchInterval,
	}
}

// GetProxy 获取一个可用的SOCKS5代理地址 (格式: "ip:port")
// 如果当前没有有效代理或者代理已过期，则尝试获取新的代理
func (m *Manager) GetProxy() (string, error) {
	m.mu.RLock()
	if m.currentProxy != "" && time.Since(m.lastFetchTime) < m.fetchInterval {
		log.Info("使用缓存代理: %s", m.currentProxy)
		proxy := m.currentProxy // 复制一份，避免在解锁后被修改
		m.mu.RUnlock()
		return proxy, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查，防止在获取锁的过程中其他goroutine已经刷新了代理
	if m.currentProxy != "" && time.Since(m.lastFetchTime) < m.fetchInterval {
		log.Info("使用缓存代理 (双重检查): %s", m.currentProxy)
		return m.currentProxy, nil
	}

	log.Info("当前无有效代理或代理已过期，正在获取新代理...")
	newProxy, err := m.fetchAndVerifyProxy()
	if err != nil {
		log.Error("获取并验证新代理失败: %v", err)
		// 即使获取失败，也更新获取时间，避免短时间内频繁尝试
		m.lastFetchTime = time.Now()
		return "", fmt.Errorf("无法获取有效代理: %w", err)
	}

	log.Info("获取到新的有效代理: %s", newProxy)
	m.currentProxy = newProxy
	m.lastFetchTime = time.Now()
	return m.currentProxy, nil
}

// fetchAndVerifyProxy 尝试获取并验证一个新的SOCKS5代理
func (m *Manager) fetchAndVerifyProxy() (string, error) {
	for i := 0; i < maxRetries; i++ {
		proxyAddr, err := m.fetchProxyFromAPI()
		if err != nil {
			log.Warn("从API获取代理失败 (尝试 %d/%d): %v", i+1, maxRetries, err)
			time.Sleep(retryDelay)
			continue
		}

		log.Info("获取到代理: %s，正在验证...", proxyAddr)
		if m.verifyProxy(proxyAddr) {
			log.Info("代理 %s 验证通过", proxyAddr)
			return proxyAddr, nil
		}
		log.Warn("代理 %s 验证失败 (尝试 %d/%d)", proxyAddr, i+1, maxRetries)
		time.Sleep(retryDelay)
	}
	return "", fmt.Errorf("尝试 %d 次后仍未获取到有效代理", maxRetries)
}

// fetchProxyFromAPI 从指定的API获取SOCKS5代理
func (m *Manager) fetchProxyFromAPI() (string, error) {
	resp, err := m.httpClient.R().Get(proxyAPIURL)
	if err != nil {
		return "", fmt.Errorf("请求代理API失败: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("代理API返回错误状态码: %d, body: %s", resp.StatusCode(), resp.String())
	}

	var proxyInfo ProxyInfo
	if err := json.Unmarshal(resp.Body(), &proxyInfo); err != nil {
		return "", fmt.Errorf("解析代理API响应失败: %w, body: %s", err, resp.String())
	}

	if proxyInfo.Code != 200 || proxyInfo.Data.Count == 0 || len(proxyInfo.Data.Proxies) == 0 {
		return "", fmt.Errorf("代理API返回无效数据: %+v", proxyInfo)
	}

	return proxyInfo.Data.Proxies[0], nil
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
	req.Header.Set("User-Agent", defaultUserAgent) // 添加User-Agent

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

// ForceRefreshProxy 强制刷新当前代理
func (m *Manager) ForceRefreshProxy() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Info("正在强制刷新代理...")
	newProxy, err := m.fetchAndVerifyProxy()
	if err != nil {
		log.Error("强制刷新代理失败: %v", err)
		// 即使获取失败，也更新获取时间，避免短时间内频繁尝试
		m.lastFetchTime = time.Now()
		return "", fmt.Errorf("无法获取有效代理: %w", err)
	}

	log.Info("强制刷新获取到新的有效代理: %s", newProxy)
	m.currentProxy = newProxy
	m.lastFetchTime = time.Now()
	return m.currentProxy, nil
}