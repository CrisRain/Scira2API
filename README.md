# Scira2API

Scira2API 是一个基于 Go 语言开发的 API 适配器服务，它提供了一个兼容 OpenAI API 的接口，将请求转发到 Scira 服务进行处理。这个项目允许使用标准的 OpenAI API 客户端与 Scira 模型进行交互。

## 📋 功能特点

- 🔄 提供兼容 OpenAI 格式的 API 接口
- 📋 支持模型列表查询（`GET /v1/models`）
- 💬 支持聊天完成接口（`POST /v1/chat/completions`）
- 🌊 支持流式响应（SSE）
- 🔑 支持 API 密钥认证
- 🔄 自动在多个用户 ID 之间轮询
- 🔧 高度可配置（通过环境变量）
- 🐳 支持 Docker 部署
- 🔄 模型名称映射（将外部模型名称映射为内部标准化名称）

## 🚀 快速开始

### 前提条件

- Go 1.23.0 或更高版本
- 有效的 Scira 用户 ID

### 安装步骤

1. 克隆仓库

```bash
git clone https://github.com/CrisRain/Scira2API.git
cd scira2api
```

2. 创建并配置 `.env` 文件

```bash
cp .env.example .env
# 编辑 .env 文件，设置必要的环境变量
```

3. 构建项目

```bash
go build -o scira2api
```

4. 运行服务

```bash
./scira2api
```

## ⚙️ 配置选项

Scira2API 通过环境变量进行配置，以下是可用的配置选项：

### 服务器配置

- `PORT`: 服务器监听端口，默认为 8080
- `READ_TIMEOUT`: 读取超时时间（秒），默认为 10
- `WRITE_TIMEOUT`: 写入超时时间（秒），默认为 10

### 认证配置

- `APIKEY`: API 密钥，用于客户端认证
- `USERIDS`: Scira 用户 ID 列表，以逗号分隔（可选，如果不提供将使用默认用户ID）

### 客户端配置

- `HTTP_PROXY`: HTTP 代理地址（可选）
- `CLIENT_TIMEOUT`: 客户端请求超时时间（秒），默认为 60
- `RETRY`: 请求失败重试次数，默认为 3
- `BASE_URL`: Scira API 基础 URL

### 模型配置

- `MODELS`: 可用模型列表，以逗号分隔
- `MODEL_MAPPING`: 模型名称映射配置，格式为 "外部模型名称:内部模型名称"，多个映射用逗号分隔

### 聊天配置

- `CHAT_DELETE`: 是否在会话结束后删除聊天记录，默认为 false

## 🐳 Docker 部署指南

### 前置要求

- Docker Engine 20.10+
- Docker Compose 2.0+
- 至少 512MB 可用内存
- 至少 1GB 可用磁盘空间

### Docker 快速开始

1. 环境配置

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
MODEL_MAPPING=claude-3.7-sonnet-thinking:scira-anthropic,gpt-4o:scira-4o
```

2. 构建和启动

```bash
# 构建并启动服务
docker-compose up -d

# 查看日志
docker-compose logs -f scira2api

# 检查服务状态
docker-compose ps
```

3. 验证部署

```bash
# 检查健康状态
curl http://localhost:8080/v1/models

# 测试聊天接口
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your_api_key" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### Docker 资源限制

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

### Docker 监控和维护

#### 健康检查

应用内置健康检查端点：
- URL: `http://localhost:8080/v1/models`
- 间隔：30秒
- 超时：10秒
- 重试：3次

#### 日志查看

```bash
# 查看实时日志
docker-compose logs -f scira2api

# 查看最近100行日志
docker-compose logs --tail=100 scira2api

# 查看特定时间段日志
docker-compose logs --since="2024-01-01T00:00:00" scira2api
```

#### 性能监控

```bash
# 查看容器资源使用情况
docker stats scira2api

# 查看容器详细信息
docker inspect scira2api
```

## 🔗 API 接口

### 获取可用模型

```
GET /v1/models
```

响应示例:

```json
{
  "object": "list",
  "data": [
    {
      "id": "scira-default",
      "created": 1714896071,
      "object": "model",
      "owned_by": "scira"
    }
  ]
}
```

### 聊天完成

```
POST /v1/chat/completions
```

请求示例:

```json
{
  "model": "gpt-4o",
  "messages": [
    {
      "role": "user",
      "content": "你好，请介绍一下自己！"
    }
  ],
  "stream": true
}
```

## 💻 系统架构与设计模式

### 高级架构

* **整体架构**：Scira2API采用了三层架构：
  * **API层**：处理HTTP请求和响应，基于Gin框架
  * **服务层**：实现业务逻辑，处理Scira API与OpenAI API格式的转换
  * **客户端层**：与Scira服务通信

* **关键组件**：
  * **Router**：处理路由请求，将请求转发到相应的处理器
  * **Middleware**：提供认证、CORS和错误处理功能
  * **Handler**：处理具体的业务逻辑
  * **Service**：提供与Scira服务通信的功能
  * **Models**：定义数据结构
  * **Config**：管理配置信息

### 使用的设计模式

* **适配器模式**：将OpenAI格式的API请求转换为Scira格式，并将Scira响应转换回OpenAI格式
* **中间件模式**：在请求处理前后执行通用操作，如认证、CORS和错误处理
* **工厂模式**：创建各种响应对象，如模型响应和聊天完成响应
* **依赖注入**：通过构造函数注入依赖，降低组件之间的耦合度
* **策略模式**：根据配置选择不同的处理策略，如选择是否在会话结束后删除聊天记录
* **映射模式**：在外部模型名称和内部模型名称之间转换，简化配置管理和扩展性

### 技术栈

* **后端**：Go语言（要求1.23.0或更高版本）
* **Web框架**：Gin（处理HTTP请求和响应）
* **配置管理**：godotenv（加载.env文件）
* **HTTP客户端**：标准库的net/http包
* **JSON处理**：标准库的encoding/json包
* **日志处理**：自定义日志包
* **基础设施**：Docker（容器化部署）

## ❓ 故障排除与常见问题

### 常见问题

1. **服务启动失败**
   - 检查 `.env` 文件配置是否正确
   - 确保所有必需的环境变量都已设置
   - 检查日志获取详细错误信息

2. **认证失败**
   - 确保 `APIKEY` 已正确设置
   - 检查请求中的 Authorization 头是否正确（格式为 `Bearer your_api_key`）

3. **模型不可用**
   - 确保 `MODELS` 环境变量包含所需的模型名称
   - 检查模型映射配置是否正确

4. **请求超时**
   - 增加 `CLIENT_TIMEOUT` 值
   - 检查网络连接和代理配置

5. **流式响应问题**
   - 确保客户端正确处理 SSE 格式的响应
   - 检查 `stream` 参数是否正确设置为 `true`

### 启用调试模式

增加日志详细程度可以帮助诊断问题：

```bash
# 在 .env 文件中添加
LOG_LEVEL=debug
```

## 🧩 项目结构

- `config/`: 配置管理
- `log/`: 日志处理
- `middleware/`: 中间件（认证、CORS、错误处理）
- `models/`: 数据模型定义
- `service/`: 核心服务实现
- `pkg/`: 辅助工具和常量

## 🤝 贡献

欢迎提交问题和拉取请求！

## 📝 许可证

本项目基于 [LICENSE](LICENSE) 文件中规定的许可证开源。