package middleware

import (
	"net/http"
	"scira2api/log"
	"scira2api/pkg/errors"

	"github.com/gin-gonic/gin"
)

// ErrorMiddleware 错误处理中间件
func ErrorMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		if apiErr, ok := recovered.(*errors.APIError); ok {
			log.Error("API error: %v", apiErr)
			SendAPIError(c, apiErr)
		} else {
			log.Error("Unexpected error: %v", recovered)
			SendErrorResponse(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		}
	})
}
