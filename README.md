# 🚀 Scira2API

[![Go Version](https://img.shields.io/github/go-mod/go-version/coderZoe/scira2api)](https://golang.org/)
[![Docker](https://img.shields.io/badge/docker-supported-blue)](https://hub.docker.com/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

> 一个高性能的 Go 语言 API 代理服务，提供与 Scira AI 的聊天交互功能，并提供 OpenAI 兼容的接口。

## 📖 项目简介

Scira2API 是一个专为 Scira AI 设计的 API 代理服务，它将 Scira AI 的接口转换为 OpenAI 兼容的格式，使开发者能够使用标准的 OpenAI SDK 与 Scira AI 进行交互。项目采用现代化的 Go 架构设计，具有高性能、高可用性和易于维护的特点。

## ✨ 主要特性

- 🔄 **OpenAI 兼容接口** - 完全兼容 OpenAI API 格式
- 🚀 **高性能代理** - 基于 Gin 框架的高性能 HTTP 服务
- 🔐 **安全认证** - 支持 Bearer Token 认证机制
- 📡 **流式响应** - 支持实时流式数据传输
- 🔁 **智能重试** - 可配置的请求重试机制
- 👥 **用户轮询** - 智能的用户 ID 轮询分配
- 🛡️ **错误处理** - 统一的错误处理和响应格式
- 📊 **日志系统** - 完善的日志记录和监控
- 🔧 **配置驱动** - 灵活的环境变量配置
- 🌐 **跨域支持** - 内置 CORS 中间件

## 🏗️ 架构概览

```
scira2api/
├── config/           # 配置管理
├── log/             # 日志系统
├── middleware/      # 中间件层
├── models/          # 数据模型
├── pkg/             # 公共包
│   ├── errors/      # 错误处理
│   └── manager/     # 管理器组件
└── service/         # 业务服务层
```

### 核心组件

- **配置管理**: 结构化配置，支持环境变量和验证
- **错误处理**: 统一的业务错误类型和 HTTP 状态映射
- **用户管理**: 线程安全的用户 ID 轮询分配
- **ID 生成器**: 多种聊天 ID 生成策略
- **中间件**: 认证、CORS、错误处理中间件
- **服务层**: 聊天处理、模型管理、请求验证

## 🚀 快速开始

### 环境要求

- Go 1.24.2 或更高版本
- 有效的 Scira AI 用户 ID

### 安装

1. 克隆项目
```bash
git clone https://github.com/crisrain/scira2api.git
cd scira2api
```

2. 安装依赖
```bash
go mod tidy
```

3. 配置环境变量
```bash
cp .env.example .env
# 编辑 .env 文件，设置必要的环境变量
```

4. 编译运行
```bash
go build -o scira2api.exe .
./scira2api.exe
```

## ⚙️ 配置说明

### 环境变量

| 变量名 | 必填 | 默认值 | 说明 |
|--------|------|--------|------|
| `PORT` | 否 | `8080` | 服务端口 |
| `APIKEY` | 否 | - | API 认证密钥 |
| `USERIDS` | 是 | - | Scira 用户 ID 列表（逗号分隔） |
| `MODELS` | 否 | `gpt-4.1-mini,claude-3-7-sonnet,grok-3-mini,qwen-qwq` | 支持的模型列表 |
| `RETRY` | 否 | `1` | 请求重试次数 |
| `HTTP_PROXY` | 否 | - | HTTP 代理地址 |
| `BASE_URL` | 否 | `https://scira.ai/` | Scira API 基础 URL |
| `CLIENT_TIMEOUT` | 否 | `300` | 客户端超时时间（秒） |
| `CHAT_DELETE` | 否 | `false` | 是否删除聊天记录 |

### 配置示例

```bash
# .env 文件示例
PORT=8080
APIKEY=your-secret-api-key
USERIDS=user1,user2,user3
MODELS=gpt-4,claude-3,grok-3-mini
RETRY=3
CLIENT_TIMEOUT=300
CHAT_DELETE=false
```

## 📚 API 文档

### 获取模型列表

```bash
GET /v1/models
```

**请求头**
```
Authorization: Bearer your-api-key
```

**响应示例**
```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4.1-mini",
      "created": 1699000000,
      "object": "model",
      "owned_by": "scira"
    }
  ]
}
```

### 聊天完成

```bash
POST /v1/chat/completions
```

**请求头**
```
Authorization: Bearer your-api-key
Content-Type: application/json
```

**请求体**
```json
{
  "model": "gpt-4.1-mini",
  "messages": [
    {
      "role": "user",
      "content": "Hello, how are you?"
    }
  ],
  "stream": false
}
```

**响应示例**
```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion",
  "created": 1699000000,
  "model": "gpt-4.1-mini",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! I'm doing well, thank you for asking."
      },
      "finish_reason": "stop"
    }
  ]
}
```

### 流式聊天

设置 `"stream": true` 以获取流式响应：

```json
{
  "model": "gpt-4.1-mini",
  "messages": [
    {
      "role": "user",
      "content": "Tell me a story"
    }
  ],
  "stream": true
}
```

## 🔧 开发指南

### 项目结构

- **config/**: 配置管理模块，处理环境变量加载和验证
- **middleware/**: HTTP 中间件，包括认证、CORS、错误处理
- **models/**: 数据模型定义，包括请求/响应结构
- **pkg/errors/**: 统一错误处理包
- **pkg/manager/**: 管理器组件，用户管理和 ID 生成
- **service/**: 业务逻辑层，处理聊天请求和响应

### 添加新功能

1. 在 `service/interfaces.go` 中定义接口
2. 在相应包中实现功能
3. 更新配置结构（如需要）
4. 添加错误类型到 `pkg/errors/`
5. 编写单元测试

### 代码风格

- 使用 `gofmt` 格式化代码
- 遵循 Go 官方编码规范
- 为公共函数添加注释
- 使用有意义的变量和函数命名

## 🧪 测试

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./pkg/manager

# 运行测试并显示覆盖率
go test -cover ./...
```

### API 测试

使用 curl 测试 API：

```bash
# 测试模型列表
curl -H "Authorization: Bearer your-api-key" \
     http://localhost:8080/v1/models

# 测试聊天完成
curl -H "Authorization: Bearer your-api-key" \
     -H "Content-Type: application/json" \
     -d '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"Hello"}]}' \
     http://localhost:8080/v1/chat/completions
```

## 🐳 Docker 部署

### 构建镜像

```bash
docker build -t scira2api .
```

### 运行容器

```bash
docker run -p 8080:8080 \
  -e USERIDS=your-user-ids \
  -e APIKEY=your-api-key \
  scira2api
```

### Docker Compose

```yaml
version: '3.8'
services:
  scira2api:
    build: .
    ports:
      - "8080:8080"
    environment:
      - PORT=8080
      - USERIDS=user1,user2,user3
      - APIKEY=your-secret-key
      - RETRY=3
    restart: unless-stopped
```

## 📊 监控和日志

### 日志级别

项目支持多种日志级别：
- `DEBUG`: 调试信息
- `INFO`: 一般信息
- `WARN`: 警告信息
- `ERROR`: 错误信息
- `FATAL`: 致命错误

### 监控指标

- 请求处理时间
- 错误率统计
- 用户 ID 使用情况
- HTTP 状态码分布

## 🔍 故障排除

### 常见问题

1. **服务启动失败**
   - 检查环境变量配置
   - 确认端口未被占用
   - 查看日志输出

2. **请求失败**
   - 验证 API 密钥
   - 检查用户 ID 配置
   - 确认网络连接

3. **性能问题**
   - 调整重试次数
   - 检查代理设置
   - 监控内存使用

### 调试模式

设置环境变量启用调试：
```bash
export LOG_LEVEL=DEBUG
```

## 🤝 贡献指南

欢迎贡献代码！请遵循以下步骤：

1. Fork 项目
2. 创建功能分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 创建 Pull Request

### 提交规范

- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更新
- `style`: 代码格式化
- `refactor`: 代码重构
- `test`: 测试相关
- `chore`: 构建过程或辅助工具的变动

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🔗 相关链接

- [Scira AI 官网](https://scira.ai/)
- [OpenAI API 文档](https://platform.openai.com/docs/)
- [Gin 框架文档](https://gin-gonic.com/)
- [Go 语言官网](https://golang.org/)

## 📞 支持

如果您遇到问题或有建议，请：

- 创建 [Issue](https://github.com/crisrain/scira2api/issues)
- 查看 [讨论区](https://github.com/crisrain/scira2api/discussions)
- 发送邮件至 support@example.com

---

⭐ 如果这个项目对您有帮助，请给我们一个 Star！