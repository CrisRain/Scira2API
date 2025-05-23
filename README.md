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

## 🚀 快速开始

### 前提条件

- Go 1.23.0 或更高版本
- 有效的 Scira 用户 ID

### 安装步骤

1. 克隆仓库

```bash
git clone https://github.com/yourusername/scira2api.git
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

### 使用 Docker 部署

1. 构建 Docker 镜像

```bash
docker build -t scira2api .
```

2. 运行容器

```bash
docker run -p 8080:8080 --env-file .env scira2api
```

或者使用 Docker Compose:

```bash
docker-compose up -d
```

详细的 Docker 部署说明请参考 [DOCKER_DEPLOYMENT.md](DOCKER_DEPLOYMENT.md)。

## ⚙️ 配置选项

Scira2API 通过环境变量进行配置，以下是可用的配置选项：

### 服务器配置

- `PORT`: 服务器监听端口，默认为 8080
- `READ_TIMEOUT`: 读取超时时间（秒），默认为 10
- `WRITE_TIMEOUT`: 写入超时时间（秒），默认为 10

### 认证配置

- `APIKEY`: API 密钥，用于客户端认证
- `USERIDS`: Scira 用户 ID 列表，以逗号分隔（必需）

### 客户端配置

- `HTTP_PROXY`: HTTP 代理地址（可选）
- `CLIENT_TIMEOUT`: 客户端请求超时时间（秒），默认为 60
- `RETRY`: 请求失败重试次数，默认为 3
- `BASE_URL`: Scira API 基础 URL

### 模型配置

- `MODELS`: 可用模型列表，以逗号分隔

### 聊天配置

- `CHAT_DELETE`: 是否在会话结束后删除聊天记录，默认为 false

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
  "model": "scira-default",
  "messages": [
    {
      "role": "user",
      "content": "你好，请介绍一下自己！"
    }
  ],
  "stream": true
}
```

## 🧩 项目结构

- `config/`: 配置管理
- `log/`: 日志处理
- `middleware/`: 中间件（认证、CORS、错误处理）
- `models/`: 数据模型定义
- `service/`: 核心服务实现
- `pkg/`: 辅助工具和常量

## 📝 许可证

本项目基于 [LICENSE](LICENSE) 文件中规定的许可证开源。

## 🤝 贡献

欢迎提交问题和拉取请求！