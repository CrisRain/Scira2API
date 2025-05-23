package connpool

import (
	"context"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"
	
	"scira2api/log"
	
	"github.com/go-resty/resty/v2"
)

// ConnPoolOptions 连接池选项
type ConnPoolOptions struct {
	MaxIdleConns        int
	MaxConnsPerHost     int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
	KeepAlive           time.Duration
	DisableCompression  bool
	DisableKeepAlives   bool
}

// ConnPoolMetrics 连接池指标
type ConnPoolMetrics struct {
	ActiveConnections   int64
	IdleConnections     int64
	ConnectionsCreated  int64
	ConnectionsClosed   int64
	ConnectionsReused   int64
	mu                  sync.RWMutex
}

// RecordActiveConn 记录活跃连接
func (m *ConnPoolMetrics) RecordActiveConn(delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ActiveConnections += delta
}

// RecordIdleConn 记录空闲连接
func (m *ConnPoolMetrics) RecordIdleConn(delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IdleConnections += delta
}

// RecordConnCreated 记录创建的连接
func (m *ConnPoolMetrics) RecordConnCreated() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConnectionsCreated++
}

// RecordConnClosed 记录关闭的连接
func (m *ConnPoolMetrics) RecordConnClosed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConnectionsClosed++
}

// RecordConnReused 记录重用的连接
func (m *ConnPoolMetrics) RecordConnReused() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConnectionsReused++
}

// GetMetrics 获取指标
func (m *ConnPoolMetrics) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return map[string]interface{}{
		"active_connections":   m.ActiveConnections,
		"idle_connections":     m.IdleConnections,
		"connections_created":  m.ConnectionsCreated,
		"connections_closed":   m.ConnectionsClosed,
		"connections_reused":   m.ConnectionsReused,
	}
}

// ConnPool 连接池管理器
type ConnPool struct {
	options *ConnPoolOptions
	metrics *ConnPoolMetrics
}

// NewConnPool 创建一个新的连接池管理器
func NewConnPool(options *ConnPoolOptions) *ConnPool {
	if options == nil {
		options = DefaultConnPoolOptions()
	}
	
	return &ConnPool{
		options: options,
		metrics: &ConnPoolMetrics{},
	}
}

// DefaultConnPoolOptions 返回默认的连接池选项
func DefaultConnPoolOptions() *ConnPoolOptions {
	// 根据CPU核心数确定最佳连接数
	numCPU := runtime.NumCPU()
	maxConnsPerHost := numCPU * 2
	maxIdleConnsPerHost := numCPU
	
	return &ConnPoolOptions{
		MaxIdleConns:        100,
		MaxConnsPerHost:     maxConnsPerHost,
		MaxIdleConnsPerHost: maxIdleConnsPerHost,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		KeepAlive:           30 * time.Second,
		DisableCompression:  false,
		DisableKeepAlives:   false,
	}
}

// ConfigureHTTPClient 配置HTTP客户端的连接池
func (p *ConnPool) ConfigureHTTPClient(client *http.Client) {
	if client.Transport == nil {
		client.Transport = &http.Transport{}
	}
	
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		log.Warn("Client transport is not *http.Transport, connection pool settings will not be applied")
		return
	}
	
	// 配置传输层参数
	transport.MaxIdleConns = p.options.MaxIdleConns
	transport.MaxConnsPerHost = p.options.MaxConnsPerHost
	transport.MaxIdleConnsPerHost = p.options.MaxIdleConnsPerHost
	transport.IdleConnTimeout = p.options.IdleConnTimeout
	transport.TLSHandshakeTimeout = p.options.TLSHandshakeTimeout
	transport.DisableCompression = p.options.DisableCompression
	transport.DisableKeepAlives = p.options.DisableKeepAlives
	
	// 使用自定义的拨号器以便跟踪连接
	if transport.DialContext != nil {
		originalDialContext := transport.DialContext
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := originalDialContext(ctx, network, addr)
			if err == nil && conn != nil {
				p.metrics.RecordConnCreated()
				p.metrics.RecordActiveConn(1)
				
				// 包装连接以跟踪关闭
				return &metricConn{
					Conn:    conn,
					metrics: p.metrics,
				}, nil
			}
			return conn, err
		}
	}
	
	log.Info("HTTP连接池配置完成: MaxIdleConns=%d, MaxConnsPerHost=%d, MaxIdleConnsPerHost=%d",
		p.options.MaxIdleConns, p.options.MaxConnsPerHost, p.options.MaxIdleConnsPerHost)
}

// ConfigureRestyClient 配置Resty客户端的连接池
func (p *ConnPool) ConfigureRestyClient(client *resty.Client) {
	// 配置底层的HTTP客户端
	p.ConfigureHTTPClient(client.GetClient())
	
	// 监控连接复用
	client.OnBeforeRequest(func(_ *resty.Client, _ *resty.Request) error {
		p.metrics.RecordConnReused()
		return nil
	})
	
	log.Info("Resty客户端连接池配置完成")
}

// GetMetrics 获取连接池指标
func (p *ConnPool) GetMetrics() map[string]interface{} {
	return p.metrics.GetMetrics()
}

// CloseIdleConnections 关闭所有空闲连接
func (p *ConnPool) CloseIdleConnections(client *http.Client) {
	if client != nil {
		client.CloseIdleConnections()
		log.Info("已关闭所有空闲连接")
	}
}

// 以下是用于跟踪连接指标的辅助类型

// metricConn 是对net.Conn的包装，用于跟踪连接指标
type metricConn struct {
	net.Conn
	metrics *ConnPoolMetrics
	closed  bool
}

// Close 关闭连接并更新指标
func (c *metricConn) Close() error {
	if !c.closed {
		c.metrics.RecordActiveConn(-1)
		c.metrics.RecordConnClosed()
		c.closed = true
	}
	return c.Conn.Close()
}