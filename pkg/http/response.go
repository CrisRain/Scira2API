package http

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// 定义响应相关的错误类型
// 优化点：添加自定义错误类型，提高错误处理的精确性和可测试性
var (
	ErrEmptyResponse      = errors.New("空响应对象")
	ErrNilResponseBody    = errors.New("响应体为空")
	ErrInvalidContentType = errors.New("无效的内容类型")
	ErrParseFailure       = errors.New("响应解析失败")
	ErrNilHTTPResponse    = errors.New("HTTP响应对象为空")
)

// ContentType 常用的内容类型
// 优化点：定义常用的内容类型常量，提高代码可读性
const (
	ContentTypeJSON       = "application/json"
	ContentTypeXML        = "application/xml"
	ContentTypeForm       = "application/x-www-form-urlencoded"
	ContentTypeHTML       = "text/html"
	ContentTypeTextPlain  = "text/plain"
	ContentTypeOctetStream = "application/octet-stream"
)

// StreamOption 定义流式处理选项
// 优化点：添加流式处理选项枚举，提供更灵活的响应处理方式
type StreamOption int

const (
	// MemoryMode 将整个响应加载到内存中（适用于小响应）
	MemoryMode StreamOption = iota
	// StreamMode 流式处理响应（适用于大型响应）
	StreamMode
)

// Response HTTP响应结构体
// 优化点：扩展结构体，添加更多有用的字段，提高功能完整性
type Response struct {
	httpResp    *http.Response  // 原始HTTP响应
	body        []byte          // 响应体数据
	request     *http.Request   // 对应的请求
	receivedAt  time.Time       // 接收响应的时间
	executionTime time.Duration // 请求执行时间
	statusText  string          // 状态码描述
	streamMode  bool            // 是否使用流式处理模式
	size        int64           // 响应大小
	err         error           // 响应过程中的错误
}

// NewResponse 创建新的响应对象
// 优化点：添加构造函数，支持流式处理选项
func NewResponse(httpResp *http.Response, request *http.Request, option StreamOption) *Response {
	response := &Response{
		httpResp:    httpResp,
		request:     request,
		receivedAt:  time.Now(),
		streamMode:  (option == StreamMode),
	}
	
	// 设置状态文本
	if httpResp != nil {
		response.statusText = http.StatusText(httpResp.StatusCode)
		response.size = httpResp.ContentLength
	}
	
	// 非流式模式下，自动解析响应体
	if !response.streamMode && httpResp != nil {
		err := response.parseBody()
		if err != nil {
			response.err = fmt.Errorf("%w: %v", ErrParseFailure, err)
		}
	}
	
	return response
}

// StatusCode 获取状态码
// 优化点：改进错误处理，添加详细注释
func (r *Response) StatusCode() int {
	if r.httpResp == nil {
		return 0
	}
	return r.httpResp.StatusCode
}

// StatusText 获取状态描述
// 优化点：新增方法，提供HTTP状态码的文本描述
func (r *Response) StatusText() string {
	return r.statusText
}

// Body 获取响应体
// 优化点：添加非空检查和错误处理
func (r *Response) Body() []byte {
	// 如果是流式模式，提醒用户应该使用RawBody
	if r.streamMode && len(r.body) == 0 {
		r.err = fmt.Errorf("流式模式下Body()可能返回空，请考虑使用RawBody()")
	}
	return r.body
}

// RawBody 获取原始响应体
// 优化点：添加详细注释，明确使用场景
func (r *Response) RawBody() io.ReadCloser {
	if r.httpResp == nil {
		return nil
	}
	return r.httpResp.Body
}

// Header 获取响应头
// 优化点：添加非空检查
func (r *Response) Header() http.Header {
	if r.httpResp == nil {
		return nil
	}
	return r.httpResp.Header
}

// IsSuccess 检查是否成功响应(2xx)
// 优化点：新增便捷方法，简化状态码检查
func (r *Response) IsSuccess() bool {
	return r.StatusCode() >= 200 && r.StatusCode() < 300
}

// IsError 检查是否错误响应(4xx/5xx)
// 优化点：新增便捷方法，简化状态码检查
func (r *Response) IsError() bool {
	return r.StatusCode() >= 400
}

// IsServerError 检查是否服务器错误(5xx)
// 优化点：新增便捷方法，简化状态码检查
func (r *Response) IsServerError() bool {
	return r.StatusCode() >= 500
}

// IsClientError 检查是否客户端错误(4xx)
// 优化点：新增便捷方法，简化状态码检查
func (r *Response) IsClientError() bool {
	return r.StatusCode() >= 400 && r.StatusCode() < 500
}

// IsRedirect 检查是否重定向响应(3xx)
// 优化点：新增便捷方法，简化状态码检查
func (r *Response) IsRedirect() bool {
	return r.StatusCode() >= 300 && r.StatusCode() < 400
}

// ContentType 获取内容类型
// 优化点：新增便捷方法，提取Content-Type头
func (r *Response) ContentType() string {
	if r.httpResp == nil {
		return ""
	}
	contentType := r.httpResp.Header.Get("Content-Type")
	if idx := strings.IndexByte(contentType, ';'); idx != -1 {
		contentType = contentType[:idx]
	}
	return strings.TrimSpace(contentType)
}

// Size 获取响应大小
// 优化点：新增方法，提供响应大小信息
func (r *Response) Size() int64 {
	if r.size > 0 {
		return r.size
	}
	return int64(len(r.body))
}

// ExecutionTime 获取执行时间
// 优化点：新增方法，提供性能指标
func (r *Response) ExecutionTime() time.Duration {
	return r.executionTime
}

// SetExecutionTime 设置执行时间
// 优化点：新增方法，支持记录请求执行时间
func (r *Response) SetExecutionTime(duration time.Duration) *Response {
	r.executionTime = duration
	return r
}

// ReceivedAt 获取接收时间
// 优化点：新增方法，提供时间信息
func (r *Response) ReceivedAt() time.Time {
	return r.receivedAt
}

// Error 获取响应过程中的错误
// 优化点：新增方法，提供错误信息
func (r *Response) Error() error {
	return r.err
}

// Request 获取对应的请求
// 优化点：新增方法，提供完整请求上下文
func (r *Response) Request() *http.Request {
	return r.request
}

// parseBody 解析响应体
// 优化点：优化内存管理，添加错误处理
func (r *Response) parseBody() error {
	if r.httpResp == nil || r.httpResp.Body == nil {
		return nil
	}
	
	// 读取响应体
	var err error
	r.body, err = io.ReadAll(r.httpResp.Body)
	if err != nil {
		return fmt.Errorf("读取响应体失败: %w", err)
	}
	
	// 关闭响应体
	err = r.httpResp.Body.Close()
	if err != nil {
		return fmt.Errorf("关闭响应体失败: %w", err)
	}
	
	return nil
}

// JSON 将响应体解析为JSON
// 优化点：新增方法，提供便捷的JSON解析
func (r *Response) JSON(v interface{}) error {
	if r.httpResp == nil {
		return ErrNilHTTPResponse
	}
	
	// 流式模式下，需要先解析响应体
	if r.streamMode && len(r.body) == 0 {
		if err := r.parseBody(); err != nil {
			return fmt.Errorf("流式模式下解析响应体失败: %w", err)
		}
	}
	
	// 检查响应体是否为空
	if len(r.body) == 0 {
		return ErrNilResponseBody
	}
	
	// 解析JSON
	err := json.Unmarshal(r.body, v)
	if err != nil {
		return fmt.Errorf("%w: JSON解析失败: %v", ErrParseFailure, err)
	}
	
	return nil
}

// XML 将响应体解析为XML
// 优化点：新增方法，提供便捷的XML解析
func (r *Response) XML(v interface{}) error {
	if r.httpResp == nil {
		return ErrNilHTTPResponse
	}
	
	// 流式模式下，需要先解析响应体
	if r.streamMode && len(r.body) == 0 {
		if err := r.parseBody(); err != nil {
			return fmt.Errorf("流式模式下解析响应体失败: %w", err)
		}
	}
	
	// 检查响应体是否为空
	if len(r.body) == 0 {
		return ErrNilResponseBody
	}
	
	// 解析XML
	err := xml.Unmarshal(r.body, v)
	if err != nil {
		return fmt.Errorf("%w: XML解析失败: %v", ErrParseFailure, err)
	}
	
	return nil
}

// String 将响应体转换为字符串
// 优化点：新增方法，提供便捷的字符串转换
func (r *Response) String() string {
	if len(r.body) == 0 {
		return ""
	}
	return string(r.body)
}

// SaveToFile 将响应体保存到文件
// 优化点：新增方法，支持将响应内容保存到文件
func (r *Response) SaveToFile(filename string) error {
	// 流式模式下的特殊处理
	if r.streamMode && r.httpResp != nil && r.httpResp.Body != nil {
		// 创建文件
		file, err := createFile(filename)
		if err != nil {
			return fmt.Errorf("创建文件失败: %w", err)
		}
		defer file.Close()
		
		// 直接从响应体流式复制到文件
		_, err = io.Copy(file, r.httpResp.Body)
		if err != nil {
			return fmt.Errorf("保存文件失败: %w", err)
		}
		
		// 关闭响应体
		r.httpResp.Body.Close()
		
		return nil
	}
	
	// 非流式模式，使用已解析的响应体
	if len(r.body) == 0 {
		return ErrNilResponseBody
	}
	
	// 创建文件并写入内容
	file, err := createFile(filename)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()
	
	_, err = file.Write(r.body)
	if err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	
	return nil
}

// createFile 创建文件的辅助函数
func createFile(filename string) (*os.File, error) {
	return os.Create(filename)
}

// Cookies 获取响应中的Cookies
// 优化点：新增方法，提供便捷的Cookie访问
func (r *Response) Cookies() []*http.Cookie {
	if r.httpResp == nil {
		return []*http.Cookie{}
	}
	return r.httpResp.Cookies()
}

// GetCookie 获取指定名称的Cookie
// 优化点：新增方法，提供便捷的Cookie查找
func (r *Response) GetCookie(name string) *http.Cookie {
	if r.httpResp == nil {
		return nil
	}
	
	for _, cookie := range r.httpResp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	
	return nil
}

// Protocol 获取响应的HTTP协议版本
// 优化点：新增方法，提供协议版本信息
func (r *Response) Protocol() string {
	if r.httpResp == nil {
		return ""
	}
	return r.httpResp.Proto
}