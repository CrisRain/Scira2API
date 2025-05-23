# Docker 部署指南

## 概述

本文档介绍如何使用 Docker 和 Docker Compose 部署 Scira2API 应用。

## 前置要求

- Docker Engine 20.10+
- Docker Compose 2.0+
- 至少 512MB 可用内存
- 至少 1GB 可用磁盘空间

## 快速开始

### 1. 环境配置

创建 `.env` 文件：

```bash
cp .env.example .env
```

编辑 `.env` 文件，设置必需的环境变量：

```env
# 必需配置
USERIDS=your_user_id_1,your_user_id_2

# 可选配置
PORT=8080
APIKEY=your_api_key
MODELS=gpt-4.1-mini,claude-3-7-sonnet,grok-3-mini,qwen-qwq
RETRY=1
BASE_URL=https://scira.ai/
CLIENT_TIMEOUT=300
CHAT_DELETE=false
```

### 2. 构建和启动

```bash
# 构建并启动服务
docker-compose up -d

# 查看日志
docker-compose logs -f scira2api

# 检查服务状态
docker-compose ps
```

### 3. 验证部署

```bash
# 检查健康状态
curl http://localhost:8080/v1/models

# 测试聊天接口
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your_api_key" \
  -d '{
    "model": "gpt-4.1-mini",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

## 配置说明

### 环境变量

| 变量名 | 必需 | 默认值 | 说明 |
|--------|------|--------|------|
| `USERIDS` | ✅ | - | Scira 用户 ID 列表（逗号分隔） |
| `PORT` | ❌ | 8080 | 服务端口 |
| `APIKEY` | ❌ | - | API 认证密钥 |
| `MODELS` | ❌ | gpt-4.1-mini,claude-3-7-sonnet,grok-3-mini,qwen-qwq | 支持的模型列表 |
| `RETRY` | ❌ | 1 | 请求重试次数 |
| `HTTP_PROXY` | ❌ | - | HTTP 代理地址 |
| `BASE_URL` | ❌ | https://scira.ai/ | Scira API 基础 URL |
| `CLIENT_TIMEOUT` | ❌ | 300 | 客户端超时时间（秒） |
| `CHAT_DELETE` | ❌ | false | 是否删除聊天记录 |

### 资源限制

默认配置：
- CPU 限制：1.0 核心
- 内存限制：512MB
- CPU 预留：0.25 核心
- 内存预留：128MB

可以在 `docker-compose.yml` 中调整：

```yaml
deploy:
  resources:
    limits:
      cpus: '2.0'
      memory: 1G
    reservations:
      cpus: '0.5'
      memory: 256M
```

## 高级配置

### 1. 自定义网络

```yaml
networks:
  scira2api-network:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/16
```

### 2. 外部数据库（如需要）

```yaml
services:
  scira2api:
    depends_on:
      - redis
    environment:
      - REDIS_URL=redis://redis:6379
  
  redis:
    image: redis:7-alpine
    volumes:
      - redis-data:/data

volumes:
  redis-data:
```

### 3. 反向代理配置

使用 Nginx：

```yaml
services:
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./ssl:/etc/nginx/ssl:ro
    depends_on:
      - scira2api
```

### 4. 日志管理

配置日志轮转：

```yaml
logging:
  driver: "json-file"
  options:
    max-size: "50m"
    max-file: "5"
    compress: "true"
```

## 监控和维护

### 健康检查

应用内置健康检查端点：
- URL: `http://localhost:8080/v1/models`
- 间隔：30秒
- 超时：10秒
- 重试：3次

### 日志查看

```bash
# 查看实时日志
docker-compose logs -f scira2api

# 查看最近100行日志
docker-compose logs --tail=100 scira2api

# 查看特定时间段日志
docker-compose logs --since="2024-01-01T00:00:00" scira2api
```

### 性能监控

```bash
# 查看容器资源使用情况
docker stats scira2api

# 查看容器详细信息
docker inspect scira2api
```

## 故障排除

### 常见问题

1. **容器启动失败**
   ```bash
   # 检查日志
   docker-compose logs scira2api
   
   # 检查配置
   docker-compose config
   ```

2. **健康检查失败**
   ```bash
   # 进入容器检查
   docker-compose exec scira2api sh
   
   # 手动测试健康检查
   wget --no-verbose --tries=1 --spider http://localhost:8080/v1/models
   ```

3. **内存不足**
   ```bash
   # 增加内存限制
   # 在 docker-compose.yml 中调整 memory 配置
   ```

### 调试模式

启用详细日志：

```yaml
environment:
  - LOG_LEVEL=debug
```

## 安全建议

1. **使用非 root 用户**：已在 Dockerfile 中配置
2. **只读文件系统**：已在 docker-compose.yml 中启用
3. **网络隔离**：使用自定义网络
4. **资源限制**：防止资源耗尽攻击
5. **定期更新**：保持基础镜像和依赖更新

## 备份和恢复

### 备份配置

```bash
# 备份配置文件
tar -czf scira2api-config-$(date +%Y%m%d).tar.gz .env docker-compose.yml

# 备份日志
tar -czf scira2api-logs-$(date +%Y%m%d).tar.gz log/
```

### 恢复服务

```bash
# 停止服务
docker-compose down

# 恢复配置
tar -xzf scira2api-config-YYYYMMDD.tar.gz

# 重新启动
docker-compose up -d
```

## 生产环境部署

### 1. 使用 Docker Swarm

```bash
# 初始化 Swarm
docker swarm init

# 部署服务
docker stack deploy -c docker-compose.yml scira2api
```

### 2. 使用 Kubernetes

参考 `k8s/` 目录中的 Kubernetes 配置文件。

### 3. CI/CD 集成

```yaml
# .github/workflows/deploy.yml
name: Deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Deploy to production
        run: |
          docker-compose -f docker-compose.prod.yml up -d
```

## 更新和升级

```bash
# 拉取最新镜像
docker-compose pull

# 重新构建并启动
docker-compose up -d --build

# 清理旧镜像
docker image prune -f
``` 