package http

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// ProxyManager 代理管理器接口
type ProxyManager interface {
	GetProxy() (string, error)
}

// HttpClient 自定义HTTP客户端，替代resty.Client
type HttpClient struct {
	client        *http.Client
	baseURL       string
	headers       map[string]string
	retryCount    int
	retryWait     time.Duration
	retryMaxWait  time.Duration
	timeout       time.Duration
	
	// 代理相关
	proxyURL      string
	dynamicProxy  bool
	proxyManager  ProxyManager
	
	// 钩子函数
	beforeRequest []func(*http.Request) error
}

// NewHttpClient 创建新的HTTP客户端
func NewHttpClient() *HttpClient {
	return &HttpClient{
		client:       &http.Client{},
		headers:      make(map[string]string),
		beforeRequest: []func(*http.Request) error{},
	}
}

// SetTimeout 设置超时时间
func (c *HttpClient) SetTimeout(timeout time.Duration) *HttpClient {
	c.timeout = timeout
	c.client.Timeout = timeout
	return c
}

// SetBaseURL 设置基础URL
func (c *HttpClient) SetBaseURL(url string) *HttpClient {
	c.baseURL = url
	return c
}

// SetHeader 设置请求头
func (c *HttpClient) SetHeader(key, value string) *HttpClient {
	c.headers[key] = value
	return c
}

// SetHeaders 设置多个请求头
func (c *HttpClient) SetHeaders(headers map[string]string) *HttpClient {
	for k, v := range headers {
		c.headers[k] = v
	}
	return c
}

// SetRetryCount 设置重试次数
func (c *HttpClient) SetRetryCount(count int) *HttpClient {
	c.retryCount = count
	return c
}

// SetRetryWaitTime 设置重试等待时间
func (c *HttpClient) SetRetryWaitTime(waitTime time.Duration) *HttpClient {
	c.retryWait = waitTime
	return c
}

// SetRetryMaxWaitTime 设置最大重试等待时间
func (c *HttpClient) SetRetryMaxWaitTime(maxWaitTime time.Duration) *HttpClient {
	c.retryMaxWait = maxWaitTime
	return c
}

// SetProxy 设置代理
func (c *HttpClient) SetProxy(proxyURL string) *HttpClient {
	c.proxyURL = proxyURL
	
	// 创建Transport
	transport := &http.Transport{}
	
	// 配置代理
	if proxyURL != "" {
		proxyFunc, err := createProxyFunc(proxyURL)
		if err == nil {
			transport.Proxy = proxyFunc
		}
	}
	
	// 更新客户端
	c.client.Transport = transport
	return c
}

// createProxyFunc 创建代理函数
func createProxyFunc(proxyURL string) (func(*http.Request) (*url.URL, error), error) {
	if proxyURL == "" {
		return nil, nil
	}
	
	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	
	return http.ProxyURL(parsedURL), nil
}

// OnBeforeRequest 添加请求前处理函数
func (c *HttpClient) OnBeforeRequest(f func(*http.Request) error) *HttpClient {
	c.beforeRequest = append(c.beforeRequest, f)
	return c
}

// R 创建请求构建器
func (c *HttpClient) R() *Request {
	return &Request{
		client:      c,
		headers:     make(map[string]string),
		queryParams: make(map[string]string),
		context:     context.Background(),
	}
}

// SetProxyManager 应用代理管理器
func (c *HttpClient) SetProxyManager(manager ProxyManager) *HttpClient {
	c.proxyManager = manager
	c.dynamicProxy = (manager != nil)
	return c
}

// SetUserAgent 设置User-Agent
func (c *HttpClient) SetUserAgent(ua string) *HttpClient {
	c.SetHeader("User-Agent", ua)
	return c
}

// EnableTrace 启用请求追踪（兼容性接口，实际不提供功能）
func (c *HttpClient) EnableTrace() *HttpClient {
	// 不实现追踪功能，仅为兼容性提供
	return c
}

// Get 发送GET请求
func (c *HttpClient) Get(url string) (*Response, error) {
	return c.R().Get(url)
}

// Post 发送POST请求
func (c *HttpClient) Post(url string, body interface{}) (*Response, error) {
	return c.R().SetBody(body).Post(url)
}
