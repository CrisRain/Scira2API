# scira2api

`scira2api` 是一个使用 Go 语言和 Gin 框架构建的高性能 API 服务。它旨在作为各类大语言模型（LLM）服务的 OpenAI 兼容前置代理和适配层。本项目提供了强大的功能，包括流式和非流式响应处理、灵活的模型映射、请求重试、响应缓存、连接池管理、速率限制以及详细的系统监控指标。

## ✨ 功能特性

-   **OpenAI API 兼容**: 完全兼容 OpenAI 的 `/v1/chat/completions` 和 `/v1/models` 端点，方便现有应用无缝迁移。
-   **灵活的模型代理与映射**: 支持将用户请求的模型名称（如 `gpt-4o`）映射到内部或实际后端使用的模型名称（如 `scira-4o`），方便统一管理和切换模型。
-   **流式与非流式响应**: 同时支持标准的 JSON 同步响应和基于 SSE (Server-Sent Events) 的异步流式响应，满足不同场景需求。
-   **强大的配置能力**: 通过环境变量（可使用 `.env` 文件）轻松配置服务的各个方面，包括：
    -   服务端口、API 密钥
    -   HTTP/SOCKS5 代理
    -   客户端超时与重试次数
    -   缓存策略（模型列表缓存、聊天响应缓存的启用与 TTL）
    -   HTTP 连接池参数
    -   API 速率限制（每秒请求数、突发容量）
    -   自定义模型映射表
-   **企业级特性**:
    -   **认证与授权**: 基于 API Key 的请求认证机制，保护您的服务。
    -   **请求重试**: 内置对后端服务调用的请求重试逻辑，提高服务调用的健壮性和成功率。
    -   **缓存机制**: 支持模型列表缓存和聊天响应内容缓存，有效降低对后端 LLM 服务的请求频率和延迟。
    -   **连接池管理**: 高效复用对后端服务的 HTTP 连接，提升性能。
    -   **速率限制**: 精细控制 API 调用频率，防止服务过载和滥用。
-   **监控与可观测性**:
    -   `/health` 端点：提供简单的服务健康状态检查。
    -   `/metrics` 端点：暴露详细的运行时指标，包括 Go 运行时信息、内存使用、GC 统计、累计请求数、成功/失败请求数，以及缓存、连接池和速率限制器的具体状态和统计数据。
-   **Token 精算**: 能够计算和校正请求与响应中的 token 数量，便于成本控制和用量分析。
-   **部署友好**: 提供 `Dockerfile`，支持容器化部署，内置健康检查指令，简化部署和运维流程。
-   **中间件支持**: 集成常用的中间件，如 CORS（跨域资源共享）处理、全局错误捕获和统一的错误响应格式化。

## 🛠️ 技术栈

-   **核心语言**: Go (1.23+)
-   **Web 框架**: Gin
-   **HTTP 客户端**: Resty v2
-   **配置管理**: godotenv (用于从 `.env` 文件加载环境变量)
-   **容器化**: Docker

## 🚀 开始使用

### 先决条件

-   Go 1.23 或更高版本
-   Docker (可选, 用于容器化部署)
-   Git

### 安装与配置

1.  **克隆仓库**:
    ```bash
    git clone https://github.com/your-username/scira2api.git # 请替换为您的仓库地址
    cd scira2api
    ```

2.  **创建并配置 `.env` 文件**:
    从 [` .env.example `](.env.example:0) 复制一份配置模板，并根据您的实际需求进行修改：
    ```bash
    cp .env.example .env
    ```
    打开 `.env` 文件并编辑以下关键配置项：
    *   `PORT`: 服务监听的端口 (默认: `8080`)。
    *   `APIKEY`: 访问受保护 API 端点（如 `/v1/chat/completions`）所需的 API 密钥。如果留空，则不启用认证。
    *   `BASE_URL`: 您要代理的后端 OpenAI 兼容 API 的基础 URL (默认: `https://api.openai.com`)。
    *   `HTTP_PROXY` / `SOCKS5_PROXY`: （可选）配置 HTTP 或 SOCKS5 代理服务器地址。
    *   `CLIENT_TIMEOUT`: 访问后端服务的 HTTP 客户端超时时间 (默认: `600s`)。
    *   `RETRY`: 访问后端服务失败时的最大重试次数 (默认: `3`)。
    *   `CACHE_ENABLED`: 是否启用缓存（包括模型列表和聊天响应）(默认: `true`)。
    *   `MODEL_CACHE_TTL`: 模型列表缓存的有效期 (默认: `1h`)。
    *   `RESP_CACHE_TTL`: 聊天响应缓存的有效期 (默认: `5m`)。
    *   `CONN_POOL_ENABLED`: 是否启用 HTTP 连接池 (默认: `true`)。
    *   `RATE_LIMIT_ENABLED`: 是否启用 API 速率限制 (默认: `true`)。
    *   `REQUESTS_PER_SECOND`: 每秒允许的平均请求数 (默认: `1`)。
    *   `BURST`: 速率限制器的突发容量 (默认: `10`)。
    *   `MODEL_MAPPINGS`: 自定义模型名称映射。格式为 `externalName1:internalName1,externalName2:internalName2`。
        例如: `gpt-4o:scira-4o,claude-3-opus:scira-anthropic-opus`。
        如果未设置或格式错误，将使用代码中定义的默认映射表。

3.  **安装依赖**:
    ```bash
    go mod tidy
    ```

### 运行应用

#### 本地运行

```bash
go run main.go
```
服务将在 `http://localhost:<PORT>` ( `<PORT>` 为您在 `.env` 中配置的端口，默认为 8080) 上启动。

#### 使用 Docker 运行

1.  **构建 Docker 镜像**:
    ```bash
    docker build -t scira2api .
    ```

2.  **运行 Docker 容器**:
    确保您的 `.env` 文件已根据需求正确配置。
    ```bash
    docker run -d -p 8080:8080 --env-file .env --name scira2api-container scira2api
    ```
    参数说明:
    *   `-d`: 后台运行容器。
    *   `-p 8080:8080`: 将主机的 8080 端口映射到容器的 8080 端口。如果您的 `PORT` 配置不同，请相应修改。
    *   `--env-file .env`: 从项目根目录下的 `.env` 文件加载环境变量到容器中。
    *   `--name scira2api-container`: 为容器指定一个易于管理的名称。

    查看容器日志:
    ```bash
    docker logs scira2api-container
    ```

## 📖 使用示例

**注意**: 以下示例中的 `YOUR_API_KEY` 需要替换为您在 `.env` 文件中配置的 `APIKEY`。如果 `APIKEY` 未设置，则无需 `Authorization` 请求头。

### 获取可用模型列表

```bash
curl http://localhost:8080/v1/models
```
如果设置了 `APIKEY` (即使 `/v1/models` 默认在认证白名单中，出于接口调用的一致性，建议也携带认证信息):
```bash
curl -H "Authorization: Bearer YOUR_API_KEY" http://localhost:8080/v1/models
```

### 发起聊天请求 (非流式)

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
-H "Authorization: Bearer YOUR_API_KEY" \
-H "Content-Type: application/json" \
-d '{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello! Can you tell me a joke?"}
  ]
}'
```

### 发起聊天请求 (流式)

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
-H "Authorization: Bearer YOUR_API_KEY" \
-H "Content-Type: application/json" \
-N \
-d '{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "Write a short story about a brave knight."}
  ],
  "stream": true
}'
```
**提示**: `-N` (或 `--no-buffer`) 选项用于 `curl` 命令，以禁用输出缓冲，从而能够即时看到流式数据的输出。

## ⚙️ API 端点

-   `GET /health`: 服务健康状态检查。
    -   响应: `{"status": "ok", "uptime": "..."}`
-   `GET /metrics`: 获取详细的性能和运行时指标。
    -   响应: 包含系统、内存、GC、请求统计、缓存、连接池、速率限制器等指标的 JSON 对象。
-   `GET /v1/models`: 获取当前配置支持的 AI 模型列表。
    -   请求头 (可选，如果 `APIKEY` 已配置): `Authorization: Bearer YOUR_API_KEY`
    -   响应: OpenAI 模型列表格式。
-   `POST /v1/chat/completions`: 发起聊天补全请求，支持流式和非流式。
    -   请求头:
        -   `Authorization: Bearer YOUR_API_KEY` (必需，如果 `APIKEY` 已配置)
        -   `Content-Type: application/json`
    -   请求体: 标准 OpenAI Chat Completions 请求格式。

## 🤝 贡献指南

我们欢迎各种形式的贡献！如果您希望为 `scira2api` 做出贡献，请遵循以下步骤：

1.  Fork 本项目仓库。
2.  从 `main` 分支创建一个新的特性分支 (例如: `git checkout -b feature/your-amazing-feature`)。
3.  进行您的修改和实现。
4.  确保您的代码通过了所有测试（如果项目包含测试）。
5.  提交您的更改 (例如: `git commit -m 'feat: Add some amazing feature'`)。
6.  将您的分支推送到 Fork 后的仓库 (例如: `git push origin feature/your-amazing-feature`)。
7.  创建一个 Pull Request 到本项目的 `main` 分支，并详细描述您的更改。

## 📄 许可证

该项目根据 MIT 许可证授权。详情请参阅 [`LICENSE`](LICENSE:0) 文件。

Copyright (c) 2025 coderZoe