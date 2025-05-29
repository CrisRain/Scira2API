package http

import (
	"context"
	"crypto/tls"
	"errors"      // 添加用于自定义错误类型
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// 定义常见错误类型，方便错误处理和测试
// 优化点：添加自定义错误类型，提高错误处理的精确性和可测试性
var (
	ErrInvalidProxyURL    = errors.New("无效的代理URL")
	ErrProxySetupFailed   = errors.New("代理设置失败")
	ErrInvalidTransport   = errors.New("无效的Transport类型")
	ErrUnsupportedScheme  = errors.New("不支持的代理协议")
)

// ProxyManager 代理管理器接口
type ProxyManager interface {
	GetProxy() (string, error)
}

// TransportConfig 传输配置
// 优化点：提取Transport配置为独立结构体，便于集中管理HTTP传输参数
type TransportConfig struct {
	// 连接池配置
	MaxIdleConns        int           // 最大空闲连接数
	MaxIdleConnsPerHost int           // 每个主机的最大空闲连接数
	MaxConnsPerHost     int           // 每个主机的最大连接数
	IdleConnTimeout     time.Duration // 空闲连接超时时间
	
	// TLS配置
	TLSConfig           *tls.Config   // TLS配置
	
	// 超时配置
	DialTimeout         time.Duration // 拨号超时时间
	DialKeepAlive       time.Duration // 保持连接超时时间
	
	// 代理配置
	ProxyURL            string        // 代理URL
	
	// HTTP/2配置
	DisableCompression  bool          // 禁用压缩
	DisableKeepAlives   bool          // 禁用长连接
	ForceAttemptHTTP2   bool          // 强制尝试HTTP/2
}

// DefaultTransportConfig 返回默认传输配置
// 优化点：提供默认值，简化客户端配置流程
func DefaultTransportConfig() *TransportConfig {
	return &TransportConfig{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DialTimeout:         30 * time.Second,
		DialKeepAlive:       30 * time.Second,
		ForceAttemptHTTP2:   true,
	}
}

// HttpClient 自定义HTTP客户端，替代resty.Client
type HttpClient struct {
	client         *http.Client
	baseURL        string
	headers        map[string]string
	retryCount     int
	retryWait      time.Duration
	retryMaxWait   time.Duration
	timeout        time.Duration
	
	// 代理相关
	proxyURL       string
	dynamicProxy   bool
	proxyManager   ProxyManager
	
	// 传输配置
	transportConfig *TransportConfig
	
	// 钩子函数
	beforeRequest   []func(*http.Request) error
	
	// 重定向处理
	disableRedirect bool
}

// NewHttpClient 创建新的HTTP客户端
// 优化点：增强初始化过程，添加默认的传输配置
func NewHttpClient() *HttpClient {
	config := DefaultTransportConfig()
	transport := createTransport(config)
	
	return &HttpClient{
		client: &http.Client{
			Transport: transport,
		},
		headers:         make(map[string]string),
		beforeRequest:   []func(*http.Request) error{},
		transportConfig: config,
	}
}

// createTransport 根据配置创建Transport
// 优化点：分离Transport创建逻辑，提高代码复用性
func createTransport(config *TransportConfig) *http.Transport {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   config.DialTimeout,
			KeepAlive: config.DialKeepAlive,
		}).DialContext,
		MaxIdleConns:        config.MaxIdleConns,
		MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     config.MaxConnsPerHost,
		IdleConnTimeout:     config.IdleConnTimeout,
		DisableCompression:  config.DisableCompression,
		DisableKeepAlives:   config.DisableKeepAlives,
		ForceAttemptHTTP2:   config.ForceAttemptHTTP2,
	}
	
	// 应用TLS配置（如果有）
	if config.TLSConfig != nil {
		transport.TLSClientConfig = config.TLSConfig
	}
	
	return transport
}

// SetTimeout 设置超时时间
// 优化点：改进命名和注释，增强可读性
func (client *HttpClient) SetTimeout(timeout time.Duration) *HttpClient {
	client.timeout = timeout
	client.client.Timeout = timeout
	return client
}

// SetBaseURL 设置基础URL
// 优化点：改进方法接收者命名
func (client *HttpClient) SetBaseURL(url string) *HttpClient {
	client.baseURL = url
	return client
}

// SetHeader 设置请求头
// 优化点：改进方法接收者命名
func (client *HttpClient) SetHeader(key, value string) *HttpClient {
	client.headers[key] = value
	return client
}

// SetHeaders 设置多个请求头
// 优化点：改进方法接收者命名
func (client *HttpClient) SetHeaders(headers map[string]string) *HttpClient {
	for k, v := range headers {
		client.headers[k] = v
	}
	return client
}

// SetRetryCount 设置重试次数
// 优化点：改进方法接收者命名，添加详细注释
func (client *HttpClient) SetRetryCount(count int) *HttpClient {
	client.retryCount = count
	return client
}

// SetRetryWaitTime 设置重试等待时间
// 优化点：改进方法接收者命名
func (client *HttpClient) SetRetryWaitTime(waitTime time.Duration) *HttpClient {
	client.retryWait = waitTime
	return client
}

// SetRetryMaxWaitTime 设置最大重试等待时间
// 优化点：改进方法接收者命名
func (client *HttpClient) SetRetryMaxWaitTime(maxWaitTime time.Duration) *HttpClient {
	client.retryMaxWait = maxWaitTime
	return client
}

// SetProxy 设置代理
// 优化点：重构复杂逻辑，提高可读性和维护性，引入错误类型
func (client *HttpClient) SetProxy(proxyURLStr string) (*HttpClient, error) {
	client.proxyURL = proxyURLStr

	// 保存当前传输配置
	if client.transportConfig == nil {
		client.transportConfig = DefaultTransportConfig()
	}
	
	// 更新传输配置中的代理URL
	client.transportConfig.ProxyURL = proxyURLStr
	
	// 创建新的Transport
	return client.applyTransportConfig()
}

// applyTransportConfig 应用传输配置
// 优化点：分离配置应用逻辑，提高代码复用性
func (client *HttpClient) applyTransportConfig() (*HttpClient, error) {
	config := client.transportConfig
	
	// 如果代理URL为空，则清除代理设置
	if config.ProxyURL == "" {
		transport := createTransport(config)
		client.client.Transport = transport
		return client, nil
	}
	
	// 解析代理URL
	parsedURL, err := url.Parse(config.ProxyURL)
	if err != nil {
		return client, fmt.Errorf("%w: %s (%v)", ErrInvalidProxyURL, config.ProxyURL, err)
	}
	
	// 获取基础Transport
	transport := createTransport(config)
	
	// 根据代理类型配置Transport
	switch strings.ToLower(parsedURL.Scheme) {
	case "http", "https":
		// 设置HTTP/HTTPS代理
		if err := configureHTTPProxy(transport, config.ProxyURL); err != nil {
			return client, fmt.Errorf("%w: %v", ErrProxySetupFailed, err)
		}
	case "socks5":
		// 设置SOCKS5代理
		if err := configureSOCKS5Proxy(transport, parsedURL.Host); err != nil {
			return client, fmt.Errorf("%w: %v", ErrProxySetupFailed, err)
		}
	default:
		return client, fmt.Errorf("%w: %s", ErrUnsupportedScheme, parsedURL.Scheme)
	}
	
	// 应用新的Transport
	client.client.Transport = transport
	return client, nil
}

// configureHTTPProxy 配置HTTP代理
// 优化点：分离HTTP代理配置逻辑，提高代码清晰度
func configureHTTPProxy(transport *http.Transport, proxyURLStr string) error {
	parsedURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return err
	}
	
	transport.Proxy = http.ProxyURL(parsedURL)
	// 确保SOCKS拨号器不被使用
	transport.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	
	return nil
}

// configureSOCKS5Proxy 配置SOCKS5代理
// 优化点：分离SOCKS5代理配置逻辑，提高代码清晰度
func configureSOCKS5Proxy(transport *http.Transport, proxyHost string) error {
	dialer, err := proxy.SOCKS5("tcp", proxyHost, nil, proxy.Direct)
	if err != nil {
		return err
	}
	
	// 转换为DialContext函数
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.Dial(network, addr)
	}
	
	// 确保HTTP代理不被使用
	transport.Proxy = nil
	
	return nil
}

// ConfigureTransport 配置传输参数
// 优化点：新增方法，允许用户完全自定义传输参数，提高灵活性
func (client *HttpClient) ConfigureTransport(config *TransportConfig) (*HttpClient, error) {
	client.transportConfig = config
	return client.applyTransportConfig()
}

// SetConnectionPool 设置连接池参数
// 优化点：新增方法，简化连接池配置，提高性能
func (client *HttpClient) SetConnectionPool(maxIdleConns, maxIdleConnsPerHost, maxConnsPerHost int) *HttpClient {
	if client.transportConfig == nil {
		client.transportConfig = DefaultTransportConfig()
	}
	
	client.transportConfig.MaxIdleConns = maxIdleConns
	client.transportConfig.MaxIdleConnsPerHost = maxIdleConnsPerHost
	client.transportConfig.MaxConnsPerHost = maxConnsPerHost
	
	// 更新Transport
	transport := createTransport(client.transportConfig)
	if client.transportConfig.ProxyURL != "" {
		// 如果已配置代理，需要重新应用代理设置
		_, _ = client.applyTransportConfig()
	} else {
		client.client.Transport = transport
	}
	
	return client
}

// SetTLSConfig 设置TLS配置
// 优化点：新增方法，支持自定义TLS配置，提高安全性和灵活性
func (client *HttpClient) SetTLSConfig(tlsConfig *tls.Config) *HttpClient {
	if client.transportConfig == nil {
		client.transportConfig = DefaultTransportConfig()
	}
	
	client.transportConfig.TLSConfig = tlsConfig
	
	// 更新Transport
	transport := createTransport(client.transportConfig)
	if client.transportConfig.ProxyURL != "" {
		// 如果已配置代理，需要重新应用代理设置
		_, _ = client.applyTransportConfig()
	} else {
		client.client.Transport = transport
	}
	
	return client
}

// DisableRedirect 禁用HTTP重定向
// 优化点：新增方法，支持控制HTTP重定向行为
func (client *HttpClient) DisableRedirect() *HttpClient {
	client.disableRedirect = true
	client.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

// EnableRedirect 启用HTTP重定向
// 优化点：新增方法，支持控制HTTP重定向行为
func (client *HttpClient) EnableRedirect() *HttpClient {
	client.disableRedirect = false
	client.client.CheckRedirect = nil
	return client
}

// OnBeforeRequest 添加请求前处理函数
// 优化点：改进方法接收者命名
func (client *HttpClient) OnBeforeRequest(f func(*http.Request) error) *HttpClient {
	client.beforeRequest = append(client.beforeRequest, f)
	return client
}

// R 创建请求构建器
// 优化点：改进方法接收者命名
func (client *HttpClient) R() *Request {
	return &Request{
		client:      client,
		headers:     make(map[string]string),
		queryParams: make(map[string]string),
		context:     context.Background(),
	}
}

// SetProxyManager 应用代理管理器
// 优化点：改进方法接收者命名
func (client *HttpClient) SetProxyManager(manager ProxyManager) *HttpClient {
	client.proxyManager = manager
	client.dynamicProxy = (manager != nil)
	return client
}

// SetUserAgent 设置User-Agent
// 优化点：改进方法接收者命名
func (client *HttpClient) SetUserAgent(ua string) *HttpClient {
	client.SetHeader("User-Agent", ua)
	return client
}

// EnableTrace 启用请求追踪（兼容性接口，实际不提供功能）
// 优化点：改进方法接收者命名，添加详细注释
func (client *HttpClient) EnableTrace() *HttpClient {
	// 不实现追踪功能，仅为兼容性提供
	// 可以在此处添加日志，提示用户该功能未实现
	return client
}

// Get 发送GET请求
// 优化点：改进方法接收者命名
func (client *HttpClient) Get(url string) (*Response, error) {
	return client.R().Get(url)
}

// Post 发送POST请求
// 优化点：改进方法接收者命名
func (client *HttpClient) Post(url string, body interface{}) (*Response, error) {
	return client.R().SetBody(body).Post(url)
}

// Put 发送PUT请求
// 优化点：新增方法，支持更多HTTP方法
func (client *HttpClient) Put(url string, body interface{}) (*Response, error) {
	return client.R().SetBody(body).Execute(http.MethodPut, url)
}

// Delete 发送DELETE请求
// 优化点：新增方法，支持更多HTTP方法
func (client *HttpClient) Delete(url string) (*Response, error) {
	return client.R().Execute(http.MethodDelete, url)
}

// Patch 发送PATCH请求
// 优化点：新增方法，支持更多HTTP方法
func (client *HttpClient) Patch(url string, body interface{}) (*Response, error) {
	return client.R().SetBody(body).Execute(http.MethodPatch, url)
}

// Head 发送HEAD请求
// 优化点：新增方法，支持更多HTTP方法
func (client *HttpClient) Head(url string) (*Response, error) {
	return client.R().Execute(http.MethodHead, url)
}

// Options 发送OPTIONS请求
// 优化点：新增方法，支持更多HTTP方法
func (client *HttpClient) Options(url string) (*Response, error) {
	return client.R().Execute(http.MethodOptions, url)
}
