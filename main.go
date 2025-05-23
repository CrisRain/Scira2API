package main

import (
	"scira2api/config"
	"scira2api/log"
	"scira2api/middleware"
	"scira2api/service"

	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// 系统状态指标
type SystemMetrics struct {
	Uptime      time.Duration     `json:"uptime"`
	NumCPU      int               `json:"num_cpu"`
	NumGoroutine int              `json:"num_goroutine"`
	MemStats    runtime.MemStats  `json:"mem_stats"`
	CacheStats  map[string]interface{} `json:"cache_stats,omitempty"`
	ConnStats   map[string]interface{} `json:"conn_stats,omitempty"`
	RateStats   map[string]interface{} `json:"rate_stats,omitempty"`
}

// 服务启动时间
var startTime time.Time

func main() {
	// 记录启动时间
	startTime = time.Now()
	// 加载配置
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatal("Failed to load config: %v", err)
	}

	// 创建处理器
	handler := service.NewChatHandler(cfg)

	// 创建路由器
	router := gin.Default()
	
	// 添加性能监控路由
	router.GET("/metrics", func(c *gin.Context) {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		
		metrics := SystemMetrics{
			Uptime:       time.Since(startTime),
			NumCPU:       runtime.NumCPU(),
			NumGoroutine: runtime.NumGoroutine(),
			MemStats:     memStats,
		}
		
		// 添加缓存指标
		metrics.CacheStats = handler.GetCacheMetrics()
		
		// 添加连接池指标
		metrics.ConnStats = handler.GetConnPoolMetrics()
		
		// 添加限流器指标
		metrics.RateStats = handler.GetRateLimiterMetrics()
		
		c.JSON(http.StatusOK, metrics)
	})

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
