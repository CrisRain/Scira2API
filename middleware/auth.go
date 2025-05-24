package middleware

import (
	"scira2api/config"
	"scira2api/pkg/errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware 认证中间件
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := cfg.Auth.ApiKey
		if apiKey != "" {
			authHeader := c.GetHeader("Authorization")
			if authHeader == "" {
				apiErr := errors.NewUnauthorizedError("Missing Authorization header")
				SendAPIError(c, apiErr)
				return
			}

			// 移除Bearer前缀
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader || token != apiKey {
				apiErr := errors.NewUnauthorizedError("Invalid API key")
				SendAPIError(c, apiErr)
				return
			}
		}
		c.Next()
	}
}
