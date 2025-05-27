package middleware

import (
	"scira2api/config"
	"scira2api/pkg/errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// 公共路径白名单，这些路径不需要认证
var publicPaths = map[string]bool{
	"/v1/models": true,
	// 可以在这里添加其他公共路径
}

// isPublicPath 检查路径是否为公共路径
func isPublicPath(path string) bool {
	return publicPaths[path]
}

// AuthMiddleware 认证中间件
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查请求路径是否是公共路径
		if isPublicPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		
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
