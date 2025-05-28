package http

import (
	"io"
	"net/http"
)

// Response HTTP响应
type Response struct {
	httpResp    *http.Response
	body        []byte
	request     *http.Request
}

// StatusCode 获取状态码
func (r *Response) StatusCode() int {
	if r.httpResp == nil {
		return 0
	}
	return r.httpResp.StatusCode
}

// Body 获取响应体
func (r *Response) Body() []byte {
	return r.body
}

// RawBody 获取原始响应体
func (r *Response) RawBody() io.ReadCloser {
	if r.httpResp == nil {
		return nil
	}
	return r.httpResp.Body
}

// Header 获取响应头
func (r *Response) Header() http.Header {
	if r.httpResp == nil {
		return nil
	}
	return r.httpResp.Header
}

// parseBody 解析响应体
func (r *Response) parseBody() error {
	if r.httpResp == nil || r.httpResp.Body == nil {
		return nil
	}
	
	var err error
	r.body, err = io.ReadAll(r.httpResp.Body)
	if err != nil {
		return err
	}
	
	r.httpResp.Body.Close()
	return nil
}