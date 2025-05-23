package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// 自定义错误类型
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
	Err     error  `json:"-"`
}

func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// 预定义错误
var (
	ErrInvalidRequest        = &APIError{Code: http.StatusBadRequest, Message: "Invalid request", Type: "invalid_request"}
	ErrUnauthorized          = &APIError{Code: http.StatusUnauthorized, Message: "Unauthorized", Type: "unauthorized"}
	ErrNotFound              = &APIError{Code: http.StatusNotFound, Message: "Not found", Type: "not_found"}
	ErrInternalServer        = &APIError{Code: http.StatusInternalServerError, Message: "Internal server error", Type: "internal_error"}
	ErrServiceUnavailable    = &APIError{Code: http.StatusServiceUnavailable, Message: "Service unavailable", Type: "service_unavailable"}
	ErrStreamingNotSupported = &APIError{Code: http.StatusInternalServerError, Message: "Streaming not supported", Type: "streaming_error"}
	ErrTooManyRequests       = &APIError{Code: http.StatusTooManyRequests, Message: "Too many requests", Type: "rate_limit_error"}
)

// 错误创建函数
func NewInvalidRequestError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusBadRequest,
		Message: message,
		Type:    "invalid_request",
		Err:     err,
	}
}

func NewUnauthorizedError(message string) *APIError {
	return &APIError{
		Code:    http.StatusUnauthorized,
		Message: message,
		Type:    "unauthorized",
	}
}

func NewInternalServerError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusInternalServerError,
		Message: message,
		Type:    "internal_error",
		Err:     err,
	}
}

func NewServiceUnavailableError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusServiceUnavailable,
		Message: message,
		Type:    "service_unavailable",
		Err:     err,
	}
}

func NewTooManyRequestsError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusTooManyRequests,
		Message: message,
		Type:    "rate_limit_error",
		Err:     err,
	}
}

// 配置错误
var (
	ErrConfigLoad       = errors.New("failed to load configuration")
	ErrConfigValidation = errors.New("configuration validation failed")
	ErrMissingUserIDs   = errors.New("USERIDS is required")
	ErrInvalidPort      = errors.New("invalid port number")
	ErrEmptyUserList    = errors.New("user list cannot be empty")
)

// HTTP 错误
var (
	ErrHTTPRequest  = errors.New("HTTP request failed")
	ErrHTTPResponse = errors.New("HTTP response error")
)

// 业务逻辑错误
var (
	ErrChatProcessing   = errors.New("chat processing failed")
	ErrStreamProcessing = errors.New("stream processing failed")
	ErrModelNotFound    = errors.New("model not found")
)
