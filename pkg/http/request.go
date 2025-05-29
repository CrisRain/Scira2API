package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"scira2api/log" // Import custom logger
	"scira2api/pkg/constants"
	"strings"
	"time"
)

// Request 请求构建器
type Request struct {
	client             *HttpClient
	method             string
	url                string
	headers            map[string]string
	queryParams        map[string]string
	body               io.Reader
	doNotParseResponse bool
	context            context.Context
}

// SetHeader 设置请求头
func (r *Request) SetHeader(key, value string) *Request {
	r.headers[key] = value
	return r
}

// SetHeaders 设置多个请求头
func (r *Request) SetHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.headers[k] = v
	}
	return r
}

// SetQueryParam 设置查询参数
func (r *Request) SetQueryParam(key, value string) *Request {
	r.queryParams[key] = value
	return r
}

// SetQueryParams 设置多个查询参数
func (r *Request) SetQueryParams(params map[string]string) *Request {
	for k, v := range params {
		r.queryParams[k] = v
	}
	return r
}

// SetBody 设置请求体
// 优化: 仅当未设置Content-Type时才设置，避免不必要的头部覆盖
func (r *Request) SetBody(body interface{}) *Request {
	switch v := body.(type) {
	case string:
		r.body = strings.NewReader(v)
	case []byte:
		r.body = bytes.NewReader(v)
	case io.Reader:
		r.body = v
	default:
		// 尝试JSON序列化
		data, err := json.Marshal(body)
		if err == nil {
			r.body = bytes.NewReader(data)
			// 仅当未设置Content-Type时才设置
			if _, hasContentType := r.headers["Content-Type"]; !hasContentType {
				r.SetHeader("Content-Type", "application/json")
			}
		}
	}
	return r
}

// SetContext 设置上下文
func (r *Request) SetContext(ctx context.Context) *Request {
	r.context = ctx
	return r
}

// SetDoNotParseResponse 设置不解析响应
func (r *Request) SetDoNotParseResponse(flag bool) *Request {
	r.doNotParseResponse = flag
	return r
}

// buildFullURL 构建完整URL
// 优化: 将URL构建逻辑分离为独立函数，简化条件判断，提高可读性
func (r *Request) buildFullURL(path string) string {
	// 如果路径已经是完整URL，直接使用
	if strings.HasPrefix(path, "http") {
		return path
	}
	
	// 如果没有基础URL，直接使用path
	if r.client.baseURL == "" {
		return path
	}
	
	// 处理基础URL和路径的斜杠问题
	baseEndsWithSlash := strings.HasSuffix(r.client.baseURL, "/")
	pathStartsWithSlash := strings.HasPrefix(path, "/")
	
	switch {
	case baseEndsWithSlash && pathStartsWithSlash:
		return r.client.baseURL + path[1:]
	case !baseEndsWithSlash && !pathStartsWithSlash:
		return r.client.baseURL + "/" + path
	default:
		return r.client.baseURL + path
	}
}

// addQueryParams 添加查询参数到URL
// 优化: 将查询参数逻辑分离，提高代码清晰度和可维护性
func (r *Request) addQueryParams(urlStr string) string {
	if len(r.queryParams) == 0 {
		return urlStr
	}
	
	// 解析原始URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// 如果解析失败，使用简单方式添加
		separator := "?"
		if strings.Contains(urlStr, "?") {
			separator = "&"
		}
		
		// 手动构建查询参数
		var parts []string
		for k, v := range r.queryParams {
			parts = append(parts, k+"="+v)
		}
		
		return urlStr + separator + strings.Join(parts, "&")
	}
	
	// 获取现有查询参数
	query := parsedURL.Query()
	
	// 添加新的查询参数
	for k, v := range r.queryParams {
		query.Add(k, v)
	}
	
	// 更新URL的查询部分
	parsedURL.RawQuery = query.Encode()
	
	return parsedURL.String()
}

// prepareRequest 创建并准备HTTP请求对象
// 优化: 将请求准备逻辑封装为单独函数，简化Execute方法
func (r *Request) prepareRequest(fullURL string) (*http.Request, error) {
	// 创建请求
	req, err := http.NewRequest(r.method, fullURL, r.body)
	if err != nil {
		return nil, fmt.Errorf("创建HTTP请求失败: %w", err)
	}
	
	// 应用上下文
	if r.context != nil {
		req = req.WithContext(r.context)
	}
	
	// 设置头信息 - 先设置客户端默认头
	for k, v := range r.client.headers {
		req.Header.Set(k, v)
	}
	
	// 然后设置请求特定头
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	
	// 设置随机User-Agent(如果未指定)
	if _, hasUserAgent := req.Header["User-Agent"]; !hasUserAgent {
		req.Header.Set("User-Agent", constants.GetRandomUserAgent())
	}
	
	// 应用请求前处理函数
	for _, f := range r.client.beforeRequest {
		if err := f(req); err != nil {
			return nil, fmt.Errorf("请求前处理失败: %w", err)
		}
	}
	
	return req, nil
}

// sendRequest 发送请求并处理响应
// 优化: 统一请求执行逻辑，减少代码重复，统一错误处理
func (r *Request) sendRequest(client *http.Client, req *http.Request, proxyType, proxyAddr string) (*Response, error) {
	// 记录请求开始
	log.Info("使用%s发送请求: %s", proxyType, proxyAddr)
	
	// 执行请求
	httpResp, err := client.Do(req)
	if err != nil {
		log.Warn("%s请求失败: %v", proxyType, err)
		return nil, fmt.Errorf("%s请求失败: %w", proxyType, err)
	}
	
	log.Info("%s请求成功，状态码: %d", proxyType, httpResp.StatusCode)
	
	// 创建响应对象
	resp := &Response{
		httpResp: httpResp,
		request:  req,
	}
	
	// 如果不需要解析响应体，直接返回
	if r.doNotParseResponse {
		log.Info("请求完成，使用: %s (%s)", proxyType, proxyAddr)
		return resp, nil
	}
	
	// 解析响应体
	if err := resp.parseBody(); err != nil {
		resp.httpResp.Body.Close()
		log.Warn("解析%s响应失败: %v", proxyType, err)
		return nil, fmt.Errorf("解析%s响应失败: %w", proxyType, err)
	}
	
	log.Info("请求完成，使用: %s (%s)", proxyType, proxyAddr)
	return resp, nil
}

// tryDynamicProxy 尝试使用动态代理
// 优化: 将动态代理逻辑封装为独立函数，便于维护和测试
func (r *Request) tryDynamicProxy(req *http.Request) (*Response, error) {
	if !r.client.dynamicProxy || r.client.proxyManager == nil {
		return nil, fmt.Errorf("动态代理未启用或代理管理器未配置")
	}
	
	// 获取动态代理地址
	log.Info("尝试获取动态代理...")
	proxyAddr, err := r.client.proxyManager.GetProxy()
	if err != nil {
		log.Warn("动态代理管理器获取代理失败: %v", err)
		return nil, fmt.Errorf("获取动态代理失败: %w", err)
	}
	
	log.Info("成功获取动态代理: %s", proxyAddr)
	
	// 解析代理URL
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		log.Warn("解析动态代理地址 %s 失败: %v", proxyAddr, err)
		return nil, fmt.Errorf("解析动态代理地址失败: %w", err)
	}
	
	// 创建临时Transport和Client
	// 优化: 只设置必要的Transport配置，减少内存分配
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	
	tempClient := &http.Client{
		Transport: transport,
		Timeout:   r.client.timeout,
	}
	
	// 使用临时客户端执行请求
	return r.sendRequest(tempClient, req, "动态代理", proxyAddr)
}

// tryStaticProxy 尝试使用静态代理
// 优化: 将静态代理逻辑封装为独立函数，提高代码可读性
func (r *Request) tryStaticProxy(req *http.Request) (*Response, error) {
	if r.client.proxyURL == "" {
		return nil, fmt.Errorf("静态代理未配置")
	}
	
	log.Info("尝试使用静态代理: %s", r.client.proxyURL)
	
	// 使用已配置代理的客户端执行请求
	return r.sendRequest(r.client.client, req, "静态代理", r.client.proxyURL)
}

// tryDirectConnection 尝试直接连接
// 优化: 将直接连接逻辑封装为独立函数，统一代理处理流程
func (r *Request) tryDirectConnection(req *http.Request) (*Response, error) {
	log.Info("尝试使用直接连接发送请求")
	
	// 使用标准客户端执行请求
	return r.sendRequest(r.client.client, req, "直接连接", "无代理")
}

// Execute 执行请求
// 优化: 将超过250行的大方法拆分为多个职责单一的小函数，提高可读性和可维护性
func (r *Request) Execute(method, path string) (*Response, error) {
	r.method = method
	
	// 构建完整URL
	fullURL := r.buildFullURL(path)
	r.url = fullURL
	
	// 添加查询参数
	fullURL = r.addQueryParams(fullURL)
	
	// 记录请求开始
	log.Info("开始处理请求: %s %s", method, fullURL)
	
	// 初始化重试计数和最后一个错误
	attempts := r.client.retryCount + 1
	var lastErr error
	
	// 重试逻辑
	for attempt := 0; attempt < attempts; attempt++ {
		// 如果不是第一次尝试，等待后重试
		if attempt > 0 {
			// 计算重试等待时间（包含抖动）
			waitTime := r.client.retryWait
			if r.client.retryMaxWait > r.client.retryWait {
				maxJitter := r.client.retryMaxWait - r.client.retryWait
				waitTime += time.Duration(rand.Int63n(int64(maxJitter)))
			}
			
			log.Warn("请求尝试 %d/%d 失败: %v. 等待 %v 后重试...",
				attempt+1, attempts, lastErr, waitTime)
			
			// 等待后重试，支持上下文取消
			select {
			case <-time.After(waitTime):
			case <-r.context.Done():
				return nil, fmt.Errorf("请求被取消: %w", r.context.Err())
			}
		}
		
		// 创建新的请求对象
		req, err := r.prepareRequest(fullURL)
		if err != nil {
			lastErr = err
			continue
		}
		
		// 代理处理策略：先尝试动态代理，再尝试静态代理，最后使用标准客户端
		var resp *Response
		
		// 1. 尝试动态代理（如果已启用）
		if r.client.dynamicProxy && r.client.proxyManager != nil {
			resp, err = r.tryDynamicProxy(req)
			if err == nil {
				return resp, nil
			}
			// 动态代理失败，继续尝试下一种连接方式
		}
		
		// 2. 尝试静态代理（如果已配置）
		if r.client.proxyURL != "" {
			resp, err = r.tryStaticProxy(req)
			if err == nil {
				return resp, nil
			}
			// 静态代理失败，继续尝试下一种连接方式
		}
		
		// 3. 尝试直接连接
		resp, err = r.tryDirectConnection(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	
	// 所有尝试都失败
	log.Error("所有请求尝试都失败了: %v", lastErr)
	if lastErr != nil {
		return nil, fmt.Errorf("请求失败: %w", lastErr)
	}
	return nil, fmt.Errorf("请求失败，无详细错误")
}

// Get 发送GET请求
func (r *Request) Get(path string) (*Response, error) {
	return r.Execute(http.MethodGet, path)
}

// Post 发送POST请求
func (r *Request) Post(path string) (*Response, error) {
	return r.Execute(http.MethodPost, path)
}