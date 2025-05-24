package middleware

import (
	"scira2api/log"
	"scira2api/pkg/errors"

	"github.com/gin-gonic/gin"
)

// 标准错误响应结构
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// 错误详情结构
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"` // HTTP状态码
}

// SendErrorResponse 发送统一格式的错误响应
func SendErrorResponse(c *gin.Context, statusCode int, errorType string, message string) {
	response := ErrorResponse{
		Error: ErrorDetail{
			Type:    errorType,
			Message: message,
			Code:    statusCode,
		},
	}
	
	// 记录错误日志
	log.Error("API错误: 类型=%s, 消息=%s, 状态码=%d", errorType, message, statusCode)
	
	c.JSON(statusCode, response)
	c.Abort()
}

// SendAPIError 发送APIError类型的错误响应
func SendAPIError(c *gin.Context, apiErr *errors.APIError) {
	SendErrorResponse(c, apiErr.Code, apiErr.Type, apiErr.Message)
}