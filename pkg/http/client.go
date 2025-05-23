package http

import (
	"time"

	"github.com/go-resty/resty/v2"
)

// Client HTTP客户端包装器
type Client struct {
	resty *resty.Client
}

// NewClient 创建新的HTTP客户端
func NewClient() *Client {
	return &Client{
		resty: resty.New(),
	}
}

// SetTimeout 设置超时时间
func (c *Client) SetTimeout(timeout time.Duration) *Client {
	c.resty.SetTimeout(timeout)
	return c
}

// SetBaseURL 设置基础URL
func (c *Client) SetBaseURL(url string) *Client {
	c.resty.SetBaseURL(url)
	return c
}

// SetProxy 设置代理
func (c *Client) SetProxy(proxy string) *Client {
	c.resty.SetProxy(proxy)
	return c
}

// SetHeader 设置请求头
func (c *Client) SetHeader(key, value string) *Client {
	c.resty.SetHeader(key, value)
	return c
}

// SetHeaders 设置多个请求头
func (c *Client) SetHeaders(headers map[string]string) *Client {
	c.resty.SetHeaders(headers)
	return c
}

// R 获取请求对象
func (c *Client) R() *resty.Request {
	return c.resty.R()
}

// GetClient 获取底层resty客户端
func (c *Client) GetClient() *resty.Client {
	return c.resty
}

// Post 发送POST请求
func (c *Client) Post(url string, body interface{}) (*resty.Response, error) {
	return c.resty.R().SetBody(body).Post(url)
}

// Get 发送GET请求
func (c *Client) Get(url string) (*resty.Response, error) {
	return c.resty.R().Get(url)
}

// SetUserAgent 设置User-Agent
func (c *Client) SetUserAgent(ua string) *Client {
	c.resty.SetHeader("User-Agent", ua)
	return c
}

// EnableTrace 启用请求追踪
func (c *Client) EnableTrace() *Client {
	c.resty.EnableTrace()
	return c
}

// SetRetryCount 设置重试次数
func (c *Client) SetRetryCount(count int) *Client {
	c.resty.SetRetryCount(count)
	return c
}

// SetRetryWaitTime 设置重试等待时间
func (c *Client) SetRetryWaitTime(waitTime time.Duration) *Client {
	c.resty.SetRetryWaitTime(waitTime)
	return c
}
