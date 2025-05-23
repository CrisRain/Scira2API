# 🚀 Scira2API

[![Go Version](https://img.shields.io/github/go-mod/go-version/coderZoe/scira2api)](https://golang.org/)
[![Docker](https://img.shields.io/badge/docker-supported-blue)](https://hub.docker.com/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

> 一个强大的API转换服务，将 [Scira AI 网页服务](https://scira.ai/) 转换为兼容 OpenAI 格式的 RESTful API，让您可以轻松集成多种AI模型到您的应用中。

## 📖 项目简介

Scira2API 是一个高性能的Go语言应用程序，它充当 Scira AI 服务的API网关，提供标准化的OpenAI兼容接口。通过这个转换层，您可以：

- 🔄 **统一接口**：使用标准的OpenAI API格式访问多种AI模型
- 🎯 **负载均衡**：支持多个用户ID的智能轮询机制  
- 🛡️ **安全可靠**：内置API密钥认证和自动重试机制
- 🌊 **实时响应**：完整支持流式输出
- 🔧 **易于部署**：提供Docker和本地部署多种方式

## ✨ 核心特性

### 🔁 智能轮询
- 支持多个 UserID 的负载均衡
- 自动故障转移机制
- 请求失败时智能重试

### 📝 会话管理  
- 自动会话创建和清理
- 可配置的聊天历史删除
- 内存高效的会话处理

### 🌊 流式支持
- 完整的SSE (Server-Sent Events) 支持
- 实时数据流传输
- 低延迟响应体验

### 🌐 网络优化
- 内置代理支持
- 高性能HTTP客户端 (基于 req/v3)
- 自动连接池管理

### 🔐 安全认证
- API密钥验证
- CORS跨域支持
- 请求头验证

## 🛠️ 技术栈

- **语言**：Go 1.24.2+
- **框架**：Gin (HTTP路由)
- **HTTP客户端**：imroc/req/v3 (高性能HTTP库)
- **配置管理**：godotenv
- **日志系统**：fatih/color (彩色日志输出)
- **容器化**：Docker & Docker Compose

## 📋 系统要求

### 最低要求
- **Go版本**：1.24+ (源码编译)
- **内存**：256MB RAM
- **存储**：50MB 磁盘空间
- **网络**：稳定的互联网连接

### 推荐配置
- **CPU**：2核心
- **内存**：512MB RAM
- **存储**：1GB 磁盘空间

## 🚀 快速开始

### 方式一：Docker 部署 (推荐)

#### 使用 Docker Run
```bash
docker run -d \
  --name scira2api \
  -p 8080:8080 \
  -e USERIDS="your_user_id_1,your_user_id_2" \
  -e APIKEY="sk-your-api-key" \
  -e CHAT_DELETE=true \
  -e HTTP_PROXY="http://127.0.0.1:7890" \
  -e MODELS="scira-anthropic,scira-4o,scira-grok-3,scira-google" \
  -e RETRY=3 \
  --restart unless-stopped \
  ghcr.io/coderzoe/scira2api:latest
```

#### 使用 Docker Compose
1. 创建 `docker-compose.yml` 文件：
```yaml
version: '3.8'

services:
  scira2api:
    image: ghcr.io/coderzoe/scira2api:latest
    container_name: scira2api
    ports:
      - "8080:8080"
    environment:
      - USERIDS=your_user_id_1,your_user_id_2  # 必填：您的Scira用户ID
      - APIKEY=sk-your-api-key                  # 可选：API访问密钥
      - CHAT_DELETE=true                        # 可选：自动删除聊天记录
      - HTTP_PROXY=http://127.0.0.1:7890       # 可选：代理设置
      - MODELS=scira-anthropic,scira-4o,scira-grok-3,scira-google  # 可选：模型列表
      - RETRY=3                                 # 可选：重试次数
      - PORT=8080                               # 可选：服务端口
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/v1/models"]
      interval: 30s
      timeout: 10s
      retries: 3
```

2. 启动服务：
```bash
docker-compose up -d
```

### 方式二：源码部署

1. **克隆项目**：
```bash
git clone https://github.com/coderZoe/scira2api.git
cd scira2api
```

2. **配置环境**：
```bash
# 复制配置文件
cp ".env copy.example" .env

# 编辑配置文件
vim .env  # 或使用您喜欢的编辑器
```

3. **编译运行**：
```bash
# 下载依赖
go mod download

# 编译二进制文件
go build -o scira2api .

# 运行服务
./scira2api
```

### 方式三：一键部署脚本

```bash
# 克隆并部署
git clone https://github.com/coderZoe/scira2api.git
cd scira2api

# 编辑配置
vim docker-compose.yml

# 一键部署
chmod +x deploy.sh
./deploy.sh
```

## ⚙️ 配置详解

### 环境变量配置

您可以通过环境变量或 `.env` 文件来配置应用程序。`.env` 文件的优先级高于环境变量。

| 变量名 | 类型 | 必填 | 默认值 | 说明 |
|--------|------|------|--------|------|
| `USERIDS` | string | ✅ | - | Scira平台的用户ID列表，用逗号分隔 |
| `PORT` | string | ❌ | `8080` | 服务监听端口 |
| `APIKEY` | string | ❌ | - | API访问密钥，为空则不验证 |
| `HTTP_PROXY` | string | ❌ | - | HTTP代理地址 |
| `MODELS` | string | ❌ | `scira-anthropic,scira-4o,scira-grok-3,scira-google` | 可用模型列表 |
| `RETRY` | int | ❌ | `0` | 请求失败重试次数 |
| `CHAT_DELETE` | bool | ❌ | `false` | 是否自动删除聊天历史 |

### 配置文件示例

创建 `.env` 文件：
```bash
# ===========================================
# Scira2API 配置文件
# ===========================================

# 【必填】Scira平台用户ID列表
# 获取方式：登录 https://mcp.scira.ai/ 后从浏览器开发者工具中获取
USERIDS=user_id_1,user_id_2,user_id_3

# 【可选】服务端口
PORT=8080

# 【可选】API访问密钥
# 设置后客户端需要在请求头中包含：Authorization: Bearer YOUR_API_KEY
APIKEY=sk-scira2api-your-secret-key

# 【可选】HTTP代理设置
# 如果您的服务器需要通过代理访问外网，请配置此项
# HTTP_PROXY=http://127.0.0.1:7890

# 【可选】可用模型列表
# 支持的模型类型，用逗号分隔
MODELS=scira-anthropic,scira-4o,scira-grok-3,scira-google

# 【可选】重试机制
# 请求失败时的重试次数，每次重试会使用不同的用户ID
RETRY=3

# 【可选】聊天历史管理
# 设置为true时，会话结束后自动删除聊天记录
CHAT_DELETE=true
```

## 📡 API 使用指南

### 认证方式

如果您设置了 `APIKEY`，需要在所有请求的头部包含认证信息：

```bash
Authorization: Bearer YOUR_API_KEY
```

### 获取模型列表

```bash
curl -X GET "http://localhost:8080/v1/models" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

**响应示例**：
```json
{
  "data": [
    {
      "id": "scira-anthropic",
      "object": "model",
      "created": 1677610602,
      "owned_by": "scira"
    },
    {
      "id": "scira-4o", 
      "object": "model",
      "created": 1677610602,
      "owned_by": "scira"
    }
  ],
  "object": "list"
}
```

### 聊天补全 (非流式)

```bash
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "scira-4o",
    "messages": [
      {
        "role": "system", 
        "content": "你是一个有用的AI助手。"
      },
      {
        "role": "user",
        "content": "请介绍一下人工智能的发展历程。"
      }
    ],
    "temperature": 0.7,
    "max_tokens": 1000
  }'
```

### 聊天补全 (流式)

```bash
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "scira-anthropic",
    "messages": [
      {
        "role": "user",
        "content": "请写一首关于春天的诗。"
      }
    ],
    "stream": true
  }'
```

**流式响应格式**：
```
data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1677610602,"model":"scira-anthropic","choices":[{"delta":{"content":"春"},"index":0,"finish_reason":null}]}

data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1677610602,"model":"scira-anthropic","choices":[{"delta":{"content":"风"},"index":0,"finish_reason":null}]}

data: [DONE]
```

### 支持的参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | ✅ | 模型名称 |
| `messages` | array | ✅ | 对话消息列表 |
| `stream` | boolean | ❌ | 是否启用流式输出 |
| `temperature` | number | ❌ | 随机性控制 (0-2) |
| `max_tokens` | integer | ❌ | 最大输出token数 |
| `top_p` | number | ❌ | 核采样参数 |
| `frequency_penalty` | number | ❌ | 频率惩罚 |
| `presence_penalty` | number | ❌ | 存在惩罚 |

## 🔧 高级配置

### 代理设置

如果您的服务器位于中国大陆或其他需要代理的地区，可以配置HTTP代理：

```bash
# 设置HTTP代理
HTTP_PROXY=http://your-proxy-server:port

# 设置SOCKS5代理  
HTTP_PROXY=socks5://your-proxy-server:port
```

### 负载均衡策略

当配置多个UserID时，系统采用轮询算法分配请求：

1. **正常情况**：按顺序轮询使用UserID
2. **故障转移**：当某个UserID请求失败时，自动切换到下一个
3. **重试机制**：每次重试使用不同的UserID，最大化成功率

### 性能优化

#### 连接池配置

系统自动管理HTTP连接池，默认配置：
- 最大空闲连接数：100
- 连接超时：30秒
- 请求超时：60秒

#### 内存优化

- 使用流式处理减少内存占用
- 自动清理过期会话
- 高效的JSON序列化

## 📊 监控与日志

### 日志级别

系统提供彩色日志输出，包含以下级别：
- **INFO**：一般信息（绿色）
- **WARN**：警告信息（黄色）  
- **ERROR**：错误信息（红色）
- **DEBUG**：调试信息（蓝色）

### 健康检查

您可以通过以下端点检查服务状态：

```bash
# 检查服务是否正常运行
curl http://localhost:8080/v1/models

# 检查具体模型可用性
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"scira-4o","messages":[{"role":"user","content":"test"}]}'
```

### Docker 健康检查

Docker部署时自动包含健康检查：

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/v1/models"]
  interval: 30s
  timeout: 10s
  retries: 3
```

## 🔍 故障排除

### 常见问题

#### 1. 服务无法启动
```bash
# 检查端口是否被占用
netstat -tlnp | grep 8080

# 检查配置文件
cat .env
```

#### 2. 请求返回401错误
- 检查APIKEY配置是否正确
- 确认请求头包含正确的Authorization字段

#### 3. 请求超时或失败
- 检查网络连接
- 验证HTTP_PROXY配置
- 确认USERIDS是否有效

#### 4. 模型不可用
- 检查MODELS配置
- 确认模型名称拼写正确
- 验证UserID权限

### 调试模式

启用详细日志：

```bash
# 设置环境变量
export GIN_MODE=debug

# 重新启动服务
./scira2api
```

### 获取帮助

如果遇到问题，请：

1. 查看项目 [Issues](https://github.com/coderZoe/scira2api/issues)
2. 提交新的 Issue 并包含：
   - 错误信息
   - 配置文件内容
   - 系统环境信息
   - 复现步骤

## 🤝 贡献指南

我们欢迎任何形式的贡献！

### 开发环境设置

1. **克隆项目**：
```bash
git clone https://github.com/coderZoe/scira2api.git
cd scira2api
```

2. **安装依赖**：
```bash
go mod download
```

3. **运行测试**：
```bash
go test ./...
```

4. **本地开发**：
```bash
# 复制配置文件
cp ".env copy.example" .env
vim .env

# 运行服务
go run main.go
```

### 提交流程

1. Fork 本仓库
2. 创建特性分支：`git checkout -b feature/amazing-feature`
3. 提交更改：`git commit -m 'Add some amazing feature'`
4. 推送分支：`git push origin feature/amazing-feature`
5. 创建 Pull Request

### 代码规范

- 遵循 [Go代码规范](https://golang.org/doc/effective_go.html)
- 添加必要的注释和文档
- 确保所有测试通过
- 更新相关文档

## 📄 许可证

本项目采用 [MIT 许可证](LICENSE)。您可以自由地使用、修改和分发此软件。

## 🙏 致谢

感谢以下开源项目：

- [Gin](https://github.com/gin-gonic/gin) - 高性能Go Web框架
- [req](https://github.com/imroc/req) - 优雅的Go HTTP客户端
- [godotenv](https://github.com/joho/godotenv) - Go环境变量加载器
- [color](https://github.com/fatih/color) - Go彩色终端输出

## 📞 联系方式

- **项目主页**：https://github.com/coderZoe/scira2api
- **作者**：[coderZoe](https://github.com/coderZoe)
- **问题反馈**：[GitHub Issues](https://github.com/coderZoe/scira2api/issues)

---

<div align="center">

**⭐ 如果这个项目对您有帮助，请给我们一个Star！**

Made with ❤️ by [coderZoe](https://github.com/coderZoe)

</div>