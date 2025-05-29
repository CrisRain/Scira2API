package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"scira2api/config"
	"scira2api/log"
	"scira2api/middleware"
	"scira2api/service"

	"github.com/gin-gonic/gin"
)

// 系统状态指标
// 优化点: 扩展系统指标收集
// 目的: 提供更全面的系统运行状态信息
// 预期效果: 更容易监控和分析系统性能
type SystemMetrics struct {
	// 基础运行时指标
	Uptime          time.Duration     `json:"uptime"`            // 服务运行时间
	StartTime       string            `json:"start_time"`        // 服务启动时间
	NumCPU          int               `json:"num_cpu"`           // CPU核心数
	NumGoroutine    int               `json:"num_goroutine"`     // Goroutine数量
	GoVersion       string            `json:"go_version"`        // Go版本
	
	// 内存指标
	MemStats        runtime.MemStats  `json:"mem_stats"`         // 内存统计
	AllocatedMem    uint64            `json:"allocated_mem"`     // 已分配内存
	TotalAllocMem   uint64            `json:"total_alloc_mem"`   // 总分配内存
	HeapObjects     uint64            `json:"heap_objects"`      // 堆对象数
	
	// GC指标
	GCPauseTotal    time.Duration     `json:"gc_pause_total"`    // GC暂停总时间
	LastGCTime      time.Time         `json:"last_gc_time"`      // 最后一次GC时间
	NumGC           uint32            `json:"num_gc"`            // GC次数
	
	// 服务指标
	RequestCount    int64             `json:"request_count"`     // 请求总数
	SuccessCount    int64             `json:"success_count"`     // 成功请求数
	ErrorCount      int64             `json:"error_count"`       // 错误请求数
	
	// 组件指标
	CacheStats      map[string]interface{} `json:"cache_stats,omitempty"`    // 缓存指标
	ConnStats       map[string]interface{} `json:"conn_stats,omitempty"`     // 连接池指标
	RateStats       map[string]interface{} `json:"rate_stats,omitempty"`     // 限流器指标
	
	// 系统负载
	LoadAverage     []float64         `json:"load_average,omitempty"`  // 系统负载平均值
}

// 服务启动时间
var (
	startTime time.Time
	// 统计信息
	requestCount int64
	successCount int64
	errorCount   int64
)

// 优化点: 重构main函数，添加优雅关闭机制
// 目的: 确保服务能够正确响应系统信号，优雅地关闭资源
// 预期效果: 提高服务稳定性，防止资源泄漏
func main() {
	// 记录启动时间
	startTime = time.Now()
	
	// 加载配置
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatal("加载配置失败: %v", err)
	}
	
	// 优化点: 启动前验证配置
	// 目的: 提前发现配置问题，避免运行时错误
	// 预期效果: 提高系统稳定性和可靠性
	if err := validateConfig(cfg); err != nil {
		log.Fatal("配置验证失败: %v", err)
	}
	
	// 创建处理器
	handler := service.NewChatHandler(cfg)
	
	// 创建路由器
	router := gin.Default()
	
	// 添加全局中间件
	setupMiddlewares(router, cfg)
	
	// 优化点: 添加请求计数中间件
	// 目的: 收集请求统计数据
	// 预期效果: 更好的监控系统性能
	router.Use(func(c *gin.Context) {
		// 请求计数器递增
		atomic.AddInt64(&requestCount, 1)
		
		// 处理请求
		c.Next()
		
		// 根据状态码更新统计
		status := c.Writer.Status()
		if status >= 200 && status < 400 {
			atomic.AddInt64(&successCount, 1)
		} else {
			atomic.AddInt64(&errorCount, 1)
		}
	})
	
	// 注册路由
	setupRoutes(router, handler)
	
	// 创建HTTP服务器
	server := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout, // 设置 IdleTimeout
	}
	
	// 优化点: 添加信号处理和优雅关闭
	// 目的: 确保系统能够正确响应系统信号，优雅地关闭
	// 预期效果: 防止资源泄漏，提高服务稳定性
	quit := make(chan os.Signal, 1)
	// 监听 SIGINT, SIGTERM 信号
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	
	// 异步启动服务器
	go func() {
		log.Info("服务器正在运行，监听端口 %s", cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("启动服务器失败: %v", err)
		}
	}()
	
	// 等待退出信号
	<-quit
	log.Info("接收到关闭信号，正在优雅关闭服务...")
	
	// 创建上下文，设置超时时间
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// 关闭HTTP服务器
	if err := server.Shutdown(ctx); err != nil {
		log.Error("服务器关闭异常: %v", err)
	}
	
	// 关闭处理器及其资源
	if err := handler.Close(); err != nil {
		log.Error("处理器资源释放失败: %v", err)
	}
	
	log.Info("服务器已成功关闭")
}

// 优化点: 分离配置验证逻辑
// 目的: 提高代码可读性，集中配置验证
// 预期效果: 更易于维护的配置验证代码
func validateConfig(cfg *config.Config) error {
	// 这里可以添加更全面的配置验证逻辑
	if cfg == nil {
		return fmt.Errorf("配置对象为空")
	}
	
	// 检查必要的配置项
	if cfg.Server.Port == "" {
		return fmt.Errorf("服务器端口未配置")
	}
	
	if cfg.Server.ReadTimeout <= 0 {
		log.Warn("读取超时设置异常，使用默认值")
	}
	
	if cfg.Server.WriteTimeout <= 0 {
		log.Warn("写入超时设置异常，使用默认值")
	}
	
	log.Info("配置验证通过")
	return nil
}

// 优化点: 分离中间件设置逻辑
// 目的: 提高代码可读性，集中中间件管理
// 预期效果: 更易于维护的中间件代码
func setupMiddlewares(router *gin.Engine, cfg *config.Config) {
	router.Use(middleware.ErrorMiddleware())
	router.Use(middleware.AuthMiddleware(cfg))
	router.Use(middleware.CorsMiddleware())
	
	// 可以在这里添加更多中间件
	log.Info("中间件设置完成")
}

// 优化点: 分离路由注册逻辑
// 目的: 提高代码可读性，降低main函数复杂度
// 预期效果: 更模块化、更易于维护的路由管理
func setupRoutes(router *gin.Engine, handler *service.ChatHandler) {
	// 添加健康检查路由
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"uptime": time.Since(startTime).String(),
		})
	})
	
	// 添加性能监控路由
	router.GET("/metrics", getMetricsHandler(handler))
	
	// API版本v1路由
	v1 := router.Group("/v1")
	{
		v1.GET("/models", handler.ModelGetHandler)
		v1.POST("/chat/completions", handler.ChatCompletionsHandler)
	}
	
	log.Info("路由注册完成")
}

// 优化点: 分离指标收集处理器
// 目的: 提高代码可读性，集中指标收集逻辑
// 预期效果: 更易于扩展和维护的指标收集代码
func getMetricsHandler(handler *service.ChatHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		
		// 获取更全面的系统指标
		metrics := SystemMetrics{
			// 基础运行时指标
			Uptime:       time.Since(startTime),
			StartTime:    startTime.Format(time.RFC3339),
			NumCPU:       runtime.NumCPU(),
			NumGoroutine: runtime.NumGoroutine(),
			GoVersion:    runtime.Version(),
			
			// 详细内存指标
			MemStats:     memStats,
			AllocatedMem: memStats.Alloc,
			TotalAllocMem: memStats.TotalAlloc,
			HeapObjects:  memStats.HeapObjects,
			
			// GC指标
			GCPauseTotal: time.Duration(memStats.PauseTotalNs),
			LastGCTime:   time.Unix(0, int64(memStats.LastGC)),
			NumGC:        memStats.NumGC,
			
			// 请求统计
			RequestCount: requestCount,
			SuccessCount: successCount,
			ErrorCount:   errorCount,
			
			// 组件指标
			CacheStats:   handler.GetCacheMetrics(),
			ConnStats:    handler.GetConnPoolMetrics(),
			RateStats:    handler.GetRateLimiterMetrics(),
		}
		
		// 添加更多指标
		if handlerMetrics := handler.GetMetrics(); handlerMetrics != nil {
			// 将处理器的指标合并到系统指标中
			for k, v := range handlerMetrics {
				if _, exists := metrics.CacheStats[k]; !exists {
					metrics.CacheStats[k] = v
				}
			}
		}
		
		c.JSON(http.StatusOK, metrics)
	}
}
