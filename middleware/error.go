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
			c.JSON(apiErr.Code, gin.H{
				"error": gin.H{
					"type":    apiErr.Type,
					"message": apiErr.Message,
				},
			})
		} else {
			log.Error("Unexpected error: %v", recovered)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"type":    "internal_error",
					"message": "Internal server error",
				},
			})
		}
		c.Abort()
	})
}
