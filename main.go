package main

import (
	"scira2api/config"
	"scira2api/log"
	"scira2api/middleware"
	"scira2api/service"

	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatal("Failed to load config: %v", err)
	}

	// 创建处理器
	handler := service.NewChatHandler(cfg)

	// 创建路由器
	router := gin.Default()

	// 添加中间件
	router.Use(middleware.ErrorMiddleware())
	router.Use(middleware.AuthMiddleware(cfg))
	router.Use(middleware.CorsMiddleware())

	// 注册路由
	v1 := router.Group("/v1")
	{
		v1.GET("/models", handler.ModelGetHandler)
		v1.POST("/chat/completions", handler.ChatCompletionsHandler)
	}

	log.Info("Server is running on port %s", cfg.Server.Port)

	// 启动服务器
	if err := router.Run(":" + cfg.Server.Port); err != nil {
		log.Fatal("Failed to start server: %v", err)
	}
}
