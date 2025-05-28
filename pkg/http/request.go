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
	"scira2api/pkg/constants"
	"strings"
	"time"
)

// Request 请求构建器
type Request struct {
	client           *HttpClient
	method           string
	url              string
	headers          map[string]string
	queryParams      map[string]string
	body             io.Reader
	doNotParseResponse bool
	context          context.Context
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
			// 设置Content-Type
			r.SetHeader("Content-Type", "application/json")
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

// Execute 执行请求
func (r *Request) Execute(method, path string) (*Response, error) {
	r.method = method
	
	// 构建完整URL
	fullURL := path
	if !strings.HasPrefix(path, "http") {
		if r.client.baseURL != "" {
			if strings.HasSuffix(r.client.baseURL, "/") && strings.HasPrefix(path, "/") {
				fullURL = r.client.baseURL + path[1:]
			} else if !strings.HasSuffix(r.client.baseURL, "/") && !strings.HasPrefix(path, "/") {
				fullURL = r.client.baseURL + "/" + path
			} else {
				fullURL = r.client.baseURL + path
			}
		}
	}
	r.url = fullURL
	
	// 添加查询参数
	if len(r.queryParams) > 0 {
		if strings.Contains(fullURL, "?") {
			fullURL += "&"
		} else {
			fullURL += "?"
		}
		
		params := url.Values{}
		for k, v := range r.queryParams {
			params.Add(k, v)
		}
		fullURL += params.Encode()
	}
	
	// 重试逻辑
	var resp *Response
	
	attempts := r.client.retryCount + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// 计算重试等待时间
			waitTime := r.client.retryWait
			if r.client.retryMaxWait > r.client.retryWait {
				maxJitter := r.client.retryMaxWait - r.client.retryWait
				waitTime += time.Duration(rand.Int63n(int64(maxJitter)))
			}
			
			// 等待后重试
			select {
			case <-time.After(waitTime):
			case <-r.context.Done():
				return nil, r.context.Err()
			}
		}
		
		// 创建请求
		req, err := http.NewRequest(method, fullURL, r.body)
		if err != nil {
			continue
		}
		
		// 应用上下文
		if r.context != nil {
			req = req.WithContext(r.context)
		}
		
		// 设置头信息
		// 首先设置客户端默认头
		for k, v := range r.client.headers {
			req.Header.Set(k, v)
		}
		// 然后设置请求特定头
		for k, v := range r.headers {
			req.Header.Set(k, v)
		}
		
		// 设置随机User-Agent
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", constants.GetRandomUserAgent())
		}
		
		// 应用请求前处理函数
		if len(r.client.beforeRequest) > 0 {
			for _, f := range r.client.beforeRequest {
				if err := f(req); err != nil {
					return nil, err
				}
			}
		}
		
		// 动态代理处理
		if r.client.dynamicProxy && r.client.proxyManager != nil {
			if proxyAddr, err := r.client.proxyManager.GetProxy(); err == nil {
				transport := &http.Transport{}
				proxyURL, _ := url.Parse(proxyAddr)
				transport.Proxy = http.ProxyURL(proxyURL)
				
				// 创建临时客户端
				client := &http.Client{
					Transport: transport,
					Timeout:   r.client.timeout,
				}
				
				// 执行请求
				httpResp, err := client.Do(req)
				if err != nil {
					continue
				}
				
				resp = &Response{
					httpResp: httpResp,
					request:  req,
				}
				
				// 处理响应
				if !r.doNotParseResponse {
					if err := resp.parseBody(); err != nil {
						httpResp.Body.Close()
						continue
					}
				}
				
				return resp, nil
			}
		}
		
		// 使用标准客户端执行请求
		httpResp, err := r.client.client.Do(req)
		if err != nil {
			continue
		}
		
		// 创建响应
		resp = &Response{
			httpResp: httpResp,
			request:  req,
		}
		
		// 处理响应
		if !r.doNotParseResponse {
			if err := resp.parseBody(); err != nil {
				httpResp.Body.Close()
				continue
			}
		}
		
		return resp, nil
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