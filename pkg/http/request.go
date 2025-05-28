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
	var err error // Declare err here to be in scope for the loop and return
	
	attempts := r.client.retryCount + 1
	for attempt := 0; attempt < attempts; attempt++ {
		// Reset err for each attempt to avoid carrying over errors from previous attempts
		// if those attempts didn't result in a return.
		// However, the primary error from client.Do or parsing should be what's checked.
		// Let's ensure 'err' is fresh or explicitly handled.
		// For now, we rely on assignments within the loop to set 'err'.

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
		req, err_req := http.NewRequest(method, fullURL, r.body)
		if err_req != nil {
			err = err_req // Assign to the loop-scoped err
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
		
		// 代理处理策略：先尝试动态代理，再尝试静态代理，最后使用标准客户端
		var proxyUsed bool = false
		// httpResp will be part of 'resp' which is declared outside the loop.
		// 'err' is also declared outside the loop.

		// 1. 尝试使用动态代理 (如果启用)
		if r.client.dynamicProxy && r.client.proxyManager != nil {
			if proxyAddr, getProxyErr := r.client.proxyManager.GetProxy(); getProxyErr == nil { // Shadow 'err' for this block
				transport := &http.Transport{}
				proxyURLVal, parseProxyErr := url.Parse(proxyAddr)
				if parseProxyErr == nil {
					transport.Proxy = http.ProxyURL(proxyURLVal)
					
					tempClient := &http.Client{
						Transport: transport,
						Timeout:   r.client.timeout,
					}
					
					var dynHttpResp *http.Response
					dynHttpResp, err = tempClient.Do(req) // Assign to loop-scoped 'err'

					if err == nil {
						proxyUsed = true
						resp = &Response{
							httpResp: dynHttpResp,
							request:  req,
						}
						if !r.doNotParseResponse {
							if parseErr := resp.parseBody(); parseErr != nil {
								resp.httpResp.Body.Close()
								proxyUsed = false
								err = parseErr
							} else {
								return resp, nil // Dynamic proxy success
							}
						} else {
							return resp, nil // Dynamic proxy success (no parsing)
						}
					}
					// If dynamic proxy HTTP call failed, 'err' is set. Loop continues.
				} else {
					log.Warn("解析动态代理地址 %s 失败: %v", proxyAddr, parseProxyErr)
					// err = parseProxyErr // Optionally set loop 'err'
				}
			} else {
				log.Warn("动态代理管理器获取代理失败: %v", getProxyErr)
				// err = getProxyErr // Optionally set loop 'err'
			}
		}

		// 2. 如果动态代理未启用/失败 (proxyUsed is false) 且配置了静态代理，则尝试静态代理
		if !proxyUsed && r.client.proxyURL != "" {
			// 静态代理已通过 SetProxy 方法配置到 r.client.client (the main client)
			// Its transport is already configured.
			var staticHttpResp *http.Response
			staticHttpResp, err = r.client.client.Do(req) // Use the main client

			if err == nil {
				proxyUsed = true
				resp = &Response{
					httpResp: staticHttpResp,
					request:  req,
				}
				if !r.doNotParseResponse {
					if parseErr := resp.parseBody(); parseErr != nil {
						resp.httpResp.Body.Close()
						proxyUsed = false
						err = parseErr
					} else {
						return resp, nil // Static proxy success
					}
				} else {
					return resp, nil // Static proxy success (no parsing)
				}
			}
			// If static proxy HTTP call failed, 'err' is set. Loop continues.
		}
		
		// 3. 如果动态代理和静态代理都未使用或失败 (proxyUsed is false)，则使用标准客户端 (无显式代理或已配置的静态代理)
		if !proxyUsed {
			// If we are here, it means either:
			// - No dynamic proxy was configured/successful.
			// - No static proxy was configured.
			// - Static proxy was configured, but the attempt failed (err from client.Do or parseBody).
			// We use r.client.client, which is the original client. If a static proxy was set via SetProxy,
			// r.client.client.Transport would already be configured with it.
			// If SetProxy was called with an empty string, r.client.client.Transport would be cleared of proxy.
			// So, this step effectively becomes "use the client as it is currently configured".
			// If a static proxy was attempted and failed above, 'err' would hold that error.
			// If no proxy was attempted, 'err' might be nil or from req creation.

			// We only proceed if 'err' from previous proxy attempts allows for a non-proxy attempt.
			// Or, more simply, always try the direct/default client if no proxy attempt succeeded.
			
			var directHttpResp *http.Response
			directHttpResp, err = r.client.client.Do(req) // This uses the client's current transport

			if err == nil {
				resp = &Response{
					httpResp: directHttpResp,
					request:  req,
				}
				if !r.doNotParseResponse {
					if parseErr := resp.parseBody(); parseErr != nil {
						resp.httpResp.Body.Close()
						err = parseErr // This error will be used for the retry loop or final return
						// continue // Let the loop handle retry based on 'err'
					} else {
						return resp, nil // Success
					}
				} else {
					return resp, nil // Success (no parsing needed)
				}
			}
			// If r.client.client.Do(req) failed, 'err' is set.
		}

		// If we've reached here, it means the current attempt (with whatever client was used)
		// resulted in an 'err'. The loop will check 'err' and decide to retry or break.
		if err != nil && attempt < attempts-1 { // Log error before retrying
			log.Warn("请求尝试 %d/%d 失败: %v. 等待 %v 后重试...", attempt+1, attempts, err, r.client.retryWait)
		}
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