# Scira2API 性能优化指南

本文档介绍了 Scira2API 项目的性能优化功能和配置方法。

## 1. 缓存功能

缓存系统用于减少重复计算和请求，提高响应速度。

### 主要特性

- **模型列表缓存**：缓存 `/v1/models` 接口返回的模型列表，减少频繁请求
- **响应缓存**：缓存非流式聊天完成请求的响应，提高响应速度
- **自动过期**：支持自定义缓存有效期
- **指标收集**：收集缓存命中率、大小等指标

### 配置选项

在 `.env` 文件中设置以下参数：

```
# 是否启用缓存
CACHE_ENABLED=true

# 模型列表缓存有效期，默认24小时
MODEL_CACHE_TTL=24h

# 响应缓存有效期，默认5分钟
RESPONSE_CACHE_TTL=5m

# 过期项清理间隔，默认10分钟
CACHE_CLEANUP_INTERVAL=10m
```

## 2. 连接池管理

连接池管理用于优化 HTTP 连接资源利用，减少连接建立和关闭的开销。

### 主要特性

- **HTTP连接池**：复用 HTTP 连接，减少连接建立开销
- **连接复用参数**：支持配置最大连接数、空闲连接超时等参数
- **连接监控**：收集连接池状态指标，如活跃连接数、空闲连接数等

### 配置选项

在 `.env` 文件中设置以下参数：

```
# 是否启用连接池
CONN_POOL_ENABLED=true

# 最大空闲连接数
MAX_IDLE_CONNS=100

# 每个主机的最大连接数
MAX_CONNS_PER_HOST=16

# 每个主机的最大空闲连接数
MAX_IDLE_CONNS_PER_HOST=8

# 空闲连接超时
IDLE_CONN_TIMEOUT=90s
```

## 3. 请求限制器

请求限制器用于控制并发请求数量，防止系统过载。

### 主要特性

- **令牌桶算法**：基于令牌桶算法的请求限制器
- **可配置速率**：支持配置每秒请求数和突发请求数
- **优雅降级**：请求被限制时会返回429状态码，而不是直接丢弃请求

### 配置选项

在 `.env` 文件中设置以下参数：

```
# 是否启用请求限制器
RATE_LIMIT_ENABLED=true

# 每秒请求数
REQUESTS_PER_SECOND=10

# 突发请求数
BURST=20
```

## 4. 性能监控

性能监控用于收集系统运行状态，帮助开发人员分析和优化系统性能。

### 主要特性

- **系统指标**：收集系统CPU、内存、goroutine数量等指标
- **组件指标**：收集缓存、连接池、请求限制器等组件的指标
- **HTTP接口**：提供 `/metrics` 接口查询性能指标

### 使用方法

访问 `/metrics` 接口获取系统性能指标，返回JSON格式数据。

示例输出：

```json
{
  "uptime": "1h30m45s",
  "num_cpu": 8,
  "num_goroutine": 15,
  "mem_stats": { ... },
  "cache_stats": {
    "enabled": true,
    "hits": 1250,
    "misses": 350,
    "size": 125,
    "hit_rate": 78.12
  },
  "conn_stats": {
    "active_connections": 5,
    "idle_connections": 10,
    "connections_created": 50,
    "connections_closed": 35,
    "connections_reused": 1500
  },
  "rate_stats": {
    "enabled": true,
    "rate": 10,
    "burst": 20,
    "available_tokens": 10,
    "request_count": 5000,
    "allowed_count": 4950,
    "rejected_count": 50,
    "rejection_rate": 1
  }
}
```

## 最佳实践

1. **适当调整缓存大小**：针对不同场景调整缓存有效期
2. **调整连接池参数**：根据服务器性能调整连接池大小，建议 `MAX_CONNS_PER_HOST = 核心数 * 2`
3. **请求限制**：根据服务器负载能力设置请求限制速率
4. **监控指标**：定期检查 `/metrics` 接口，了解系统性能状况
5. **禁用功能**：在调试或低负载环境中，可以禁用部分功能以简化系统

## 问题排查

1. **缓存问题**：如果缓存未生效，检查 `CACHE_ENABLED` 配置和缓存有效期
2. **连接池问题**：如果连接池未复用连接，检查连接池配置和客户端配置
3. **请求被限制**：如果请求返回429状态码，说明请求超过了限制速率，可以调整 `REQUESTS_PER_SECOND` 参数
4. **性能问题**：通过 `/metrics` 接口查看系统状态，定位性能瓶颈