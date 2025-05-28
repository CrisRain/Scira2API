package http

import (
	"context"
	"fmt"     // Added for error formatting
	"net"     // Added for net.Conn
	"net/http"
	"net/url"
	"strings" // Added for scheme checking
	"time"

	"golang.org/x/net/proxy" // Added for SOCKS5 proxy
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
// Note: The 'proxy.SOCKS5' call below is from the original user snippet.
// The 'proxy' package and 'proxy.SOCKS5' function are not defined in standard Go
// or in the provided file's imports. This will cause a compilation error unless
// a specific 'proxy' package providing this function is imported and works as expected.
func (c *HttpClient) SetProxy(proxyURLStr string) (*HttpClient, error) {
	c.proxyURL = proxyURLStr

	if proxyURLStr == "" {
		// Clear proxy settings
		if existingTransport, ok := c.client.Transport.(*http.Transport); ok && existingTransport != nil {
			clonedTransport := existingTransport.Clone()
			clonedTransport.Proxy = nil
			clonedTransport.DialContext = nil // Reset to default dial behavior
			c.client.Transport = clonedTransport
		} else {
			// If not an *http.Transport or c.client.Transport was nil,
			// create a new default transport (or reset to http.DefaultTransport).
			c.client.Transport = &http.Transport{}
		}
		return c, nil
	}

	parsedURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return c, fmt.Errorf("invalid proxy URL '%s': %w", proxyURLStr, err)
	}

	// Initialize or clone transport to preserve other settings
	newTransport := &http.Transport{} // Default new transport
	if c.client.Transport != nil {
		if T, ok := c.client.Transport.(*http.Transport); ok {
			newTransport = T.Clone() // Clone if existing is *http.Transport
		} else {
			// If existing transport is not *http.Transport, we cannot reliably set a proxy on it.
			return c, fmt.Errorf("cannot set proxy: existing client transport is of type %T, not *http.Transport", c.client.Transport)
		}
	}

	switch strings.ToLower(parsedURL.Scheme) {
	case "http", "https":
		proxyFunc, err_http := createProxyFunc(proxyURLStr) // createProxyFunc is defined below
		if err_http != nil {
			return c, fmt.Errorf("failed to create HTTP/S proxy func for '%s': %w", proxyURLStr, err_http)
		}
		newTransport.Proxy = proxyFunc
		newTransport.DialContext = nil // Ensure SOCKS dialer (if any) is not used
	case "socks5":
		// Use proxy.SOCKS5 from golang.org/x/net/proxy
		// parsedURL.Host should contain the SOCKS5 server address (host:port)
		// Assuming no authentication for SOCKS5 proxy for now (auth = nil)
		dialer, err_socks := proxy.SOCKS5("tcp", parsedURL.Host, nil, proxy.Direct)
		if err_socks != nil {
			return c, fmt.Errorf("failed to initialize SOCKS5 dialer for '%s' (%s): %w", proxyURLStr, parsedURL.Host, err_socks)
		}
		// The dialer returned by proxy.SOCKS5 is of type proxy.Dialer,
		// which has a Dial(network, address string) (net.Conn, error) method.
		newTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// The proxy.Dialer interface's Dial method does not take a context.
			// net.Dialer (which proxy.Direct uses) respects context passed to http.Request.
			return dialer.Dial(network, addr)
		}
		newTransport.Proxy = nil // Ensure HTTP proxy (if any) is not used
	default:
		return c, fmt.Errorf("unsupported proxy scheme '%s' in URL '%s'", parsedURL.Scheme, proxyURLStr)
	}

	c.client.Transport = newTransport
	return c, nil
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
