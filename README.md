# Scira2API

Scira2API 是一个基于 Go 语言开发的 API 适配器服务。它的核心目标是提供一个与 OpenAI API 100% 兼容的接口，使开发者能够无缝地使用现有的 OpenAI 生态系统工具与后端的 Scira 大语言模型进行交互。本项目充当 Scira 模型与 OpenAI API 标准之间的桥梁或适配器，旨在简化集成流程并提升开发效率。

## 📋 功能特点

-   **🔄 OpenAI API 兼容**:
    -   提供与 OpenAI API 规范一致的 `/v1/models` 和 `/v1/chat/completions` 端点。
    -   支持标准的请求和响应格式，方便现有应用迁移。
-   **🌊 流式响应 (SSE)**:
    -   `/v1/chat/completions` 接口支持通过 Server-Sent Events (SSE) 进行流式响应 (`stream: true`)。
    -   SSE 流包含心跳机制以维持长连接，并在每个数据块中提供实时的 Token 使用情况。
    -   流结束时发送标准 `data: [DONE]\n\n` 标志。
-   **🔑 API 密钥认证**:
    -   通过 HTTP `Authorization: Bearer <API_KEY>` 头部进行认证。
    -   可通过 `APIKEY` 环境变量配置密钥；若为空，则禁用认证。
    -   `/v1/models`, `/health`, `/metrics` 接口默认公开，无需认证。
-   **🧑‍🤝‍🧑 多用户 ID 轮询**:
    -   通过 `USERIDS` 环境变量配置一个或多个 Scira 用户 ID。
    -   服务会在这些 `USERIDS` 之间轮询，用于向 Scira 服务发起请求。若未配置，则使用内部默认 UserID。
-   **📊 Tokens 统计与校正**:
    -   服务会本地近似计算请求的输入 (prompt) Tokens 和响应的输出 (completion) Tokens。
    -   本地计算的 Tokens 会与从 Scira 服务获取的 Tokens 用量进行比较和校正，以提供更准确的统计。
    -   最终的 `usage` 对象 (`prompt_tokens`, `completion_tokens`, `total_tokens`) 会包含在 API 响应中。
-   **🧩 模型名称映射**:
    -   支持将用户请求中使用的外部模型名称 (如 `gpt-4o`) 映射到 Scira 服务实际使用的内部模型名称 (如 `scira-4o`)。
    -   当前版本的映射关系主要通过项目内置的 `config/model_mapping.go` 文件硬编码实现。关于通过 `MODEL_MAPPING` 环境变量自定义此行为，请参见[配置选项](#%EF%B8%8F-配置选项)部分的详细说明。
-   **🔗 代理支持**:
    -   支持通过 HTTP 代理 (`HTTP_PROXY`) 或 SOCKS5 代理 (`SOCKS5_PROXY`) 连接 Scira 服务。
    -   支持动态代理池 (`DYNAMIC_PROXY: true`)，可配置代理刷新间隔 (`PROXY_REFRESH_MIN`)。
-   **🗄️ 响应缓存**:
    -   对 `/v1/models` 接口的响应进行缓存 (可配置缓存时间 `MODEL_CACHE_TTL`)。
    -   对 `/v1/chat/completions` 的**非流式**请求响应进行缓存 (可配置缓存时间 `RESP_CACHE_TTL`)。
    -   缓存功能可通过 `CACHE_ENABLED` 环境变量控制，并可配置清理间隔 `CLEANUP_INTERVAL`。
-   **♻️ HTTP 连接池**:
    -   对发往 Scira 服务的 HTTP 请求使用连接池进行管理，以提高性能和资源利用率。
    -   连接池参数 (如最大连接数、空闲连接超时等) 可通过环境变量配置。
    -   连接池功能可通过 `CONN_POOL_ENABLED` 环境变量控制。
-   **🚦 速率限制**:
    -   对 API 请求进行速率限制，以防止滥用和保障服务稳定。
    -   限制参数 (如每秒请求数 `REQUESTS_PER_SECOND`、突发量 `BURST`) 可通过环境变量配置。
    -   速率限制功能可通过 `RATE_LIMIT_ENABLED` 环境变量控制。
-   **🩺 健康检查与监控**:
    -   提供 `/health` 端点用于简单的健康检查，返回服务状态和运行时间。
    -   提供 `/metrics` 端点用于获取详细的运行时性能指标，包括 Go 运行时、内存、GC、请求统计以及缓存、连接池、限流器等组件指标。
-   **📜 自定义日志系统**:
    -   使用带级别的彩色控制台日志，方便调试和追踪。
    -   日志级别可通过 `LOG_LEVEL` 环境变量配置 (如 `debug`, `info`, `warn`, `error`)。
-   **✨ `reasoning_content` 字段**:
    -   API 响应 (流式和非流式) 的消息体中可能包含一个 Scira 服务特有的 `reasoning_content` 字段，透传额外信息。
-   **🐳 Docker 部署**:
    -   提供 `Dockerfile` 和 `docker-compose.yml` 文件，支持快速、便捷的容器化部署。
    -   Docker 镜像经过优化，基于 Alpine Linux，体积小巧。

## 🚀 快速开始

### 前提条件

-   Go 1.23.0 或更高版本 (若本地构建)
-   有效的 Scira 用户 ID

### 本地安装与运行

1.  **克隆仓库**

    ```bash
    git clone https://github.com/CrisRain/Scira2API.git
    cd scira2api
    ```

2.  **创建并配置 `.env` 文件**
    从 `.env.example` 复制一份作为 `.env` 文件，并根据您的需求编辑必要的环境变量，尤其是 `USERIDS`。

    ```bash
    cp .env.example .env
    # 编辑 .env 文件，设置 USERIDS 等
    ```

3.  **构建项目**

    ```bash
    go build -o scira2api .
    ```

4.  **运行服务**

    ```bash
    ./scira2api
    ```
    服务默认启动在 `8080` 端口，或您在 `.env` 文件中通过 `PORT` 指定的端口。

## 🐳 Docker 部署指南

使用 Docker 是推荐的部署方式，可以简化环境配置和管理。

### 前置要求

-   Docker Engine 20.10+
-   Docker Compose 2.0+
-   至少 512MB 可用内存 (推荐 1GB)
-   至少 1GB 可用磁盘空间

### Docker 快速开始

1.  **环境配置**
    与本地运行类似，首先复制并配置 `.env` 文件。确保 `USERIDS` 已正确设置。

    ```bash
    cp .env.example .env
    # 编辑 .env 文件，设置必要的环境变量，例如：
    # USERIDS=your_user_id_1,your_user_id_2
    # PORT=8080
    # APIKEY=your_secure_api_key
    # ... 其他配置项
    ```

2.  **构建和启动服务**

    ```bash
    # 使用 docker-compose 构建镜像并启动服务 (推荐)
    docker-compose up -d

    # 或者，如果您想手动构建和运行：
    # docker build -t scira2api:latest .
    # docker run -d --name scira2api -p 8080:8080 --env-file .env scira2api:latest
    ```

3.  **验证部署**
    服务启动后，可以通过以下方式验证：

    ```bash
    # 1. 检查 Docker 容器日志
    docker-compose logs -f scira2api

    # 2. 检查健康状态端点 (端口可能根据您的 PORT 配置而变化)
    curl http://localhost:8080/health

    # 3. 获取模型列表 (如果配置了 APIKEY，请添加认证头)
    curl http://localhost:8080/v1/models # (如果 APIKEY 未设置或为空)
    # curl -H "Authorization: Bearer your_api_key" http://localhost:8080/v1/models # (如果 APIKEY 已设置)

    # 4. 测试聊天接口 (替换 your_api_key 和 model)
    curl -X POST http://localhost:8080/v1/chat/completions \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer your_api_key" \
      -d '{
        "model": "gpt-4o",
        "messages": [{"role": "user", "content": "Hello from Scira2API!"}],
        "stream": false
      }'
    ```

### Docker 资源限制

`docker-compose.yml` 文件中已配置了默认的资源限制和预留，您可以根据实际需求进行调整：

```yaml
# docker-compose.yml
services:
  scira2api:
    # ...
    deploy:
      resources:
        limits:
          cpus: '1.0' # CPU 核心数限制
          memory: 512M # 内存限制
        reservations:
          cpus: '0.25' # CPU 核心数预留
          memory: 128M # 内存预留
```

### Docker 监控和维护

#### 健康检查

-   **Dockerfile 内置健康检查:** `Dockerfile` 使用 `HEALTHCHECK` 指令定期请求 `http://localhost:8080/v1/models` (容器内) 来检查服务健康状态。
-   **docker-compose 健康检查:** `docker-compose.yml` 也配置了类似的健康检查。
-   **手动检查:** 您可以访问 `/health` 端点获取服务状态。

#### 日志查看

```bash
# 查看实时日志
docker-compose logs -f scira2api

# 查看最近 N 行日志
docker-compose logs --tail=100 scira2api

# 查看特定时间段后的日志
docker-compose logs --since="2024-05-30T00:00:00" scira2api
```

#### 性能监控

```bash
# 查看容器资源使用情况 (CPU, 内存, 网络 IO, 磁盘 IO)
docker stats scira2api

# 查看容器详细信息，包括网络配置、挂载卷等
docker inspect scira2api

# 应用内置性能指标端点
curl http://localhost:8080/metrics # (如果 APIKEY 未设置或为空)
# curl -H "Authorization: Bearer your_api_key" http://localhost:8080/metrics # (如果 APIKEY 已设置)
```
此端点提供详细的 Go 运行时指标、内存使用、垃圾回收统计、请求计数以及项目各组件（如缓存、连接池、速率限制器）的性能数据。

## ⚙️ 配置选项

Scira2API 通过环境变量进行配置。您可以在项目根目录创建一个 `.env` 文件来管理这些变量，服务启动时会自动加载。环境变量的优先级高于 `.env` 文件中的设置。

以下是可用的配置选项及其详细说明：

---

### Ⅰ. 服务器与认证 (`ServerConfig`, `AuthConfig`)

| 环境变量          | 描述                                                                 | 格式/示例                            | 默认值 (代码层面)        |
| :---------------- | :------------------------------------------------------------------- | :----------------------------------- | :----------------------- |
| `PORT`            | 应用监听的 HTTP 端口。                                                 | `8080`                               | `8080`                   |
| `READ_TIMEOUT`    | 服务器读取请求的超时时间。                                               | `10s`, `1m`                          | `10s` (来自常量)         |
| `WRITE_TIMEOUT`   | 服务器写入响应的超时时间。                                               | `10s`, `1m`                          | `10s` (来自常量)         |
| `IDLE_TIMEOUT`    | 服务器保持空闲 HTTP 连接的超时时间。                                     | `300s`, `5m`                         | `300s` (来自常量)        |
| `APIKEY`          | 客户端访问 API 的密钥 (Bearer Token)。如果为空字符串，则禁用认证。       | `sk-yoursecureapikey`                | `""` (空字符串，即禁用)  |
| `USERIDS`         | **(必需)** Scira 服务的用户 ID 列表，用英文逗号分隔，用于轮询。           | `user1,user2,user3`                  | 无 (若未提供，会用内部默认) |
| `LOG_LEVEL`       | 日志输出级别。可选值: `debug`, `info`, `warn`, `error`, `fatal`。       | `info`                               | `info`                   |

---

### Ⅱ. Scira 客户端与代理 (`ClientConfig`)

| 环境变量            | 描述                                                                   | 格式/示例                                  | 默认值 (代码层面)                  |
| :------------------ | :--------------------------------------------------------------------- | :----------------------------------------- | :------------------------------- |
| `BASE_URL`          | Scira API 服务的基地址。                                                 | `https://scira.ai/`                        | `https://scira.ai/`              |
| `CLIENT_TIMEOUT`    | 请求 Scira 服务的整体超时时间。                                          | `300s`, `5m`                               | `300s` (来自常量)                |
| `RETRY`             | 请求 Scira 服务失败时的重试次数。实际最小重试次数为1。                       | `3`                                        | `1` (`docker-compose` 默认)     |
| `HTTP_PROXY`        | 用于连接 Scira 服务的 HTTP 代理地址。                                    | `http://user:pass@proxy.example.com:8080`  | `""` (不使用)                  |
| `SOCKS5_PROXY`      | 用于连接 Scira 服务的 SOCKS5 代理地址。                                  | `socks5://user:pass@proxy.example.com:1080`| `""` (不使用)                  |
| `DYNAMIC_PROXY`     | 是否启用动态代理池 (需要配合代理提供程序，当前代码未包含具体实现细节)。    | `true` / `false`                           | `false`                          |
| `PROXY_REFRESH_MIN` | (若启用动态代理) 动态代理池的刷新间隔。                                | `30m`, `1h`                                | `30m`                            |

---

### Ⅲ. 聊天与模型配置 (`ChatConfig`, 模型相关)

| 环境变量        | 描述                                                                                                                                                              | 格式/示例                                            | 默认值 (代码层面)        |
| :-------------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------- | :--------------------------------------------------- | :----------------------- |
| `CHAT_DELETE`   | 是否在会话结束后删除聊天记录 (此选项的具体行为可能依赖 Scira 服务特性)。                                                                                                 | `true` / `false`                                     | `false`                  |
| `MODEL_MAPPING` | **模型名称映射**。格式为 `external_name1:internal_name1,external_name2:internal_name2`。                                                                             | `gpt-4o:scira-4o,claude-3-opus:scira-claude-opus`    | (见下方重要说明)         |
| `MODELS`        | (旧版 README 提及) 用于配置可用模型列表。**重要说明**: 当前代码分析显示，`/v1/models` API 返回的模型列表主要由 `config/model_mapping.go` 文件中硬编码的映射决定。通过此环境变量配置模型列表的机制尚不明确或可能未被当前代码逻辑直接使用。建议依赖默认的模型列表或通过修改源码中的硬编码映射。 | `model1,model2`                                      | (依赖硬编码映射)         |

**关于 `MODEL_MAPPING` 环境变量的重要说明:**
虽然 `.env.example` 和一些文档提及了 `MODEL_MAPPING` 环境变量，但根据当前代码 (`config/config.go` 和 `config/model_mapping.go`) 的分析，**服务启动时主要依赖于 `config/model_mapping.go` 文件中硬编码的默认模型映射关系。**
目前**未发现**明确的代码逻辑会使用 `MODEL_MAPPING` 环境变量的值来覆盖或扩展这个硬编码的映射。
因此，如果您希望自定义模型映射：
1.  **推荐方式 (当前有效):** 直接修改项目源码中的 `config/model_mapping.go` 文件，然后重新编译和部署服务。
2.  **环境变量方式 (效果待验证):** 您可以尝试设置 `MODEL_MAPPING` 环境变量，但其是否能按预期生效取决于是否有未被分析到的代码逻辑或未来的功能更新。

---

### Ⅳ. 缓存配置 (`CacheConfig`)

| 环境变量           | 描述                                          | 格式/示例        | 默认值 (代码层面) |
| :----------------- | :-------------------------------------------- | :--------------- | :---------------- |
| `CACHE_ENABLED`    | 是否启用缓存功能 (模型列表缓存和非流式响应缓存)。 | `true` / `false` | `true`            |
| `MODEL_CACHE_TTL`  | `/v1/models` 接口响应的缓存有效期。             | `1h`, `30m`      | `1h`              |
| `RESP_CACHE_TTL`   | 非流式 `/v1/chat/completions` 响应的缓存有效期。| `5m`, `10m`      | `5m`              |
| `CLEANUP_INTERVAL` | 缓存清理任务的运行间隔。                        | `10m`, `30m`     | `10m`             |

---

### Ⅴ. HTTP 连接池配置 (`ConnPoolConfig`)

| 环境变量                  | 描述                                                    | 格式/示例        | 默认值 (代码层面)        |
| :------------------------ | :------------------------------------------------------ | :--------------- | :----------------------- |
| `CONN_POOL_ENABLED`       | 是否启用对 Scira 服务的 HTTP 连接池。                   | `true` / `false` | `true`                   |
| `MAX_IDLE_CONNS`          | 连接池中允许的最大空闲连接总数。                          | `1000`           | `1000`                   |
| `MAX_CONNS_PER_HOST`      | 连接池对每个目标主机允许的最大连接数。                      | `100`            | (CPU核心数 * 2)          |
| `MAX_IDLE_CONNS_PER_HOST` | 连接池对每个目标主机允许的最大空闲连接数。                  | `50`             | (CPU核心数)              |
| `IDLE_CONN_TIMEOUT`       | 连接池中空闲连接被关闭前的最长等待时间。                    | `90s`, `2m`      | `90s`                    |

---

### Ⅵ. 速率限制配置 (`RateLimitConfig`)

| 环境变量              | 描述                                          | 格式/示例        | 默认值 (代码层面) |
| :-------------------- | :-------------------------------------------- | :--------------- | :---------------- |
| `RATE_LIMIT_ENABLED`  | 是否启用 API 请求速率限制。                     | `true` / `false` | `true`            |
| `REQUESTS_PER_SECOND` | 每秒允许的平均请求数 (基于 Token Bucket 算法)。 | `1.0`, `0.5`     | `1.0`             |
| `BURST`               | 允许的突发请求量 (Token Bucket 的容量)。        | `10`             | `10`              |

*(注意: 上述默认值部分基于代码分析，部分可能来自 `scira2api/pkg/constants` 包。实际生效的默认值以代码为准。)*

## 🔗 API 接口文档

### 认证

-   **方式**: API 密钥认证。客户端需在 HTTP 请求的 `Authorization` 头部包含一个 Bearer Token。
    ```
    Authorization: Bearer YOUR_API_KEY
    ```
-   **启用**: 通过设置 `APIKEY` 环境变量来提供密钥。如果 `APIKEY` 为空或未设置，则认证被禁用，所有受保护的接口都可以匿名访问。
-   **公共路径**: 以下路径默认不需要认证：
    -   `GET /health`
    -   `GET /metrics`
    -   `GET /v1/models`

### 端点详情

#### 1. 健康检查

-   **`GET /health`**
    -   **描述**: 返回服务的健康状态和基本运行信息。
    -   **认证**: 无需。
    -   **响应示例**:
        ```json
        {
          "status": "OK",
          "uptime": "1h2m3s", // 服务运行时间
          "version": "dev", // 或实际版本号
          "start_time": "YYYY-MM-DDTHH:MM:SSZ"
        }
        ```

#### 2. 性能指标

-   **`GET /metrics`**
    -   **描述**: 返回服务详细的运行时性能指标，格式与 Prometheus 兼容。包括 Go 运行时统计 (内存、GC 等)、请求计数、以及缓存、连接池、限流器等组件的内部指标。
    -   **认证**: 若 `APIKEY` 已配置，则需要认证；否则无需。
    -   **响应示例**: Prometheus 文本格式的指标数据。

#### 3. 获取可用模型列表

-   **`GET /v1/models`**
    -   **描述**: 返回当前服务支持的可用 AI 模型列表。此接口的响应会被缓存以提高性能 (缓存时间由 `MODEL_CACHE_TTL` 控制)。
    -   **认证**: 无需。
    -   **响应结构 (`models.OpenAIModelResponse`):**
        ```json
        {
          "object": "list",
          "data": [
            {
              "id": "external-model-name-1", // 例如: "gpt-4o"
              "object": "model",
              "created": 1677610600, // Unix 时间戳
              "owned_by": "scira" // 或其他所有者标识
            },
            {
              "id": "external-model-name-2",
              "object": "model",
              "created": 1677610601,
              "owned_by": "scira"
            }
            // ...更多模型
          ]
        }
        ```
    -   **数据来源**: 模型列表主要基于项目内部 `config/model_mapping.go` 中定义的硬编码模型映射。

#### 4. 聊天完成

-   **`POST /v1/chat/completions`**
    -   **描述**: 根据用户提供的对话消息和模型，生成聊天回复。支持流式 (SSE) 和非流式响应。
    -   **认证**: 若 `APIKEY` 已配置，则需要认证；否则无需。
    -   **请求体参数校验 (`service/validator.go`):**
        -   `model` (string): **必需**。必须是 `/v1/models` 返回的有效模型 ID 之一。
        -   `messages` (array): **必需**，且不能为空。
        -   每个 `message` 对象:
            -   `role` (string): **必需** (例如: "user", "assistant", "system")。
            -   `content` (string): **必需**。
    -   **请求体 JSON (`models.OpenAIChatCompletionsRequest`):**
        ```json
        {
          "model": "gpt-4o",
          "messages": [
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "Hello! Can you tell me a joke?"}
          ],
          "stream": false, // 设置为 true 以启用流式响应
          // ... 其他 OpenAI 兼容参数，如 temperature, top_p, max_tokens 等 (注意: 项目对这些额外参数的支持程度需参照 Scira 服务本身)
        }
        ```
    -   **非流式响应 (`stream: false`) (`models.OpenAIChatCompletionsResponse`):**
        ```json
        {
          "id": "chatcmpl-xxxxxxxxxxxxxxxxxxxxxx", // 聊天完成 ID
          "object": "chat.completion",
          "created": 1677652288, // Unix 时间戳
          "model": "gpt-4o", // 使用的模型
          "choices": [
            {
              "index": 0,
              "message": {
                "role": "assistant",
                "content": "\n\nHello there! Here's a joke for you: Why don't scientists trust atoms? Because they make up everything!",
                "reasoning_content": "Optional Scira-specific reasoning details." // 可能存在
              },
              "finish_reason": "stop" // 或 "length", "content_filter", "tool_calls"
            }
          ],
          "usage": {
            "prompt_tokens": 9,
            "completion_tokens": 20,
            "total_tokens": 29
          }
        }
        ```
        此响应可能会被缓存 (如果缓存已启用，由 `RESP_CACHE_TTL` 控制)。

    -   **流式响应 (`stream: true`) (SSE, `models.OpenAIChatCompletionsStreamResponse` per chunk):**
        客户端将收到一系列 Server-Sent Events。每个事件的数据部分是一个 JSON 对象。
        ```
        data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}],"usage":null}

        data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}],"usage":{"prompt_tokens": 9, "completion_tokens": 1, "total_tokens": 10}}

        data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":" there!"},"finish_reason":null}],"usage":{"prompt_tokens": 9, "completion_tokens": 3, "total_tokens": 12}}

        # ... 更多内容块，可能包含 reasoning_content in delta
        # delta.reasoning_content: "Some reasoning text chunk."

        data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens": 9, "completion_tokens": 20, "total_tokens": 29}} // 最终校正后的 usage

        data: [DONE]
        ```
        -   **心跳机制**: 服务会定期发送 SSE 心跳注释 (例如 `: ping`) 来维持连接。
        -   **`usage`**: 每个包含内容 (`delta.content`) 的数据块都会更新 `usage` 字段，提供实时的 Token 消耗。最后一个数据块会包含最终校正后的 `usage`。
        -   **`reasoning_content`**: `delta` 对象中也可能包含 `reasoning_content` 字段，分块提供 Scira 特有的推理信息。
        -   **结束标志**: 流结束时，会发送一个 `data: [DONE]\n\n` 事件。

### 错误响应格式

当 API 请求发生错误时，服务会返回一个标准化的 JSON 错误响应 (HTTP 状态码通常为 4xx 或 5xx)。
结构 (`middleware/response.go` 中的 `ErrorResponse`):

```json
{
  "error": {
    "message": "A human-readable description of the error.",
    "type": "error_type_code", // 例如: "invalid_request_error", "authentication_error", "internal_error"
    "param": "parameter_name", // (可选) 导致错误的参数名
    "code": "specific_error_code" // (可选) 更具体的错误代码，或有时重复 HTTP 状态码
  }
}
```
例如，认证失败时 (HTTP 401 Unauthorized):
```json
{
  "error": {
    "message": "Incorrect API key provided. You can find your API key at https://platform.openai.com/account/api-keys.", // 消息可能通用化
    "type": "authentication_error",
    "param": null,
    "code": 401
  }
}
```
参数校验失败时 (HTTP 400 Bad Request):
```json
{
  "error": {
    "message": "model is required",
    "type": "invalid_request_error",
    "param": "model",
    "code": 400
  }
}
```

## ✨ 高级特性与内部机制

### 1. 代理支持 (Proxy)

Scira2API 支持通过多种代理方式连接到后端的 Scira 服务，增强了网络访问的灵活性和可控性。

-   **静态 HTTP/SOCKS5 代理**:
    -   通过 `HTTP_PROXY` 环境变量配置 HTTP/HTTPS 代理。
    -   通过 `SOCKS5_PROXY` 环境变量配置 SOCKS5 代理。
    -   代理地址格式通常为 `scheme://[user:password@]host:port`。
-   **动态代理池**:
    -   通过设置 `DYNAMIC_PROXY=true` 启用。
    -   服务会（理论上）从一个可动态更新的代理池中选择代理。具体实现和代理源依赖于 `proxy.Manager` 的具体逻辑（当前代码库中 `proxy/proxy_manager.go` 可能需要用户自行实现或扩展代理获取逻辑）。
    -   `PROXY_REFRESH_MIN` 控制代理池的刷新间隔。
-   **代理优先级**: 如果配置了多种代理，其生效优先级通常是：动态代理 > SOCKS5 代理 > HTTP 代理。

### 2. 响应缓存 (Caching)

为了提升性能和减少对 Scira 服务的请求压力，Scira2API 内置了响应缓存机制。

-   **启用/禁用**: 通过 `CACHE_ENABLED` (默认为 `true`) 控制。
-   **模型列表缓存**: `/v1/models` 接口的响应会被缓存，缓存有效期由 `MODEL_CACHE_TTL` (默认 `1h`) 控制。
-   **聊天响应缓存**: **非流式**的 `/v1/chat/completions` 请求的成功响应会被缓存，缓存有效期由 `RESP_CACHE_TTL` (默认 `5m`) 控制。缓存键基于请求的 `model` 和 `messages` 内容。
-   **缓存清理**: 后台会定期清理过期的缓存条目，清理间隔由 `CLEANUP_INTERVAL` (默认 `10m`) 控制。

### 3. HTTP 连接池 (Connection Pooling)

Scira2API 使用 HTTP 连接池来管理对下游 Scira 服务的出站连接，这有助于：
-   减少连接建立的延迟。
-   复用 TCP 连接，提高网络效率。
-   控制并发连接数，避免耗尽本地或远程资源。

-   **启用/禁用**: 通过 `CONN_POOL_ENABLED` (默认为 `true`) 控制。
-   **可配置参数**:
    -   `MAX_IDLE_CONNS`: 最大空闲连接总数。
    -   `MAX_CONNS_PER_HOST`: 每台目标主机的最大连接数。
    -   `MAX_IDLE_CONNS_PER_HOST`: 每台目标主机的最大空闲连接数。
    -   `IDLE_CONN_TIMEOUT`: 空闲连接的超时时间。

### 4. 速率限制 (Rate Limiting)

为防止 API 被滥用并确保服务的稳定性，Scira2API 实现了基于 Token Bucket 算法的速率限制器。

-   **启用/禁用**: 通过 `RATE_LIMIT_ENABLED` (默认为 `true`) 控制。
-   **可配置参数**:
    -   `REQUESTS_PER_SECOND`: 每秒允许通过的平均请求数。
    -   `BURST`: 令牌桶的容量，即允许的瞬时突发请求量。
-   当请求超过限制时，API 会返回 HTTP `429 Too Many Requests` 错误。

### 5. Tokens 计算与校正

Scira2API 在处理聊天请求时，会进行 Tokens 统计，并努力提供准确的用量信息。

-   **本地近似计算**:
    -   服务会使用内部算法 (`service/utils.go` 中的 `countTokens` 和 `calculateMessageTokens`) 对请求中的输入消息 (prompt) 和生成的输出消息 (completion) 进行**近似的** Tokens 数量估算。这种估算基于对单词、标点和 CJK 字符的经验规则。
-   **与 Scira 服务用量校正**:
    -   Scira 服务本身也会返回其计算的 Tokens 用量。
    -   Scira2API 会将本地计算的 Tokens 与 Scira 服务返回的用量进行比较和校正 (`service.ChatHandler.correctUsage()`)。
    -   最终在 API 响应的 `usage` 字段中（包括流式响应的每个数据块和最终块），会提供一个尽可能准确的 Tokens 统计。
-   **实时 Token 消耗 (流式)**: 对于流式响应，每个包含文本增量的数据块都会更新 `usage` 字段，让客户端可以实时了解 Token 的消耗情况。

### 6. `reasoning_content` 字段

在与 Scira 服务交互时，Scira 可能返回一些额外的推理过程或元数据信息。Scira2API 会将这些信息通过一个名为 `reasoning_content` 的字段透传给客户端。

-   此字段可能出现在**非流式响应**的 `message.reasoning_content` 中。
-   也可能出现在**流式响应**的 `delta.reasoning_content` 中，分块提供。
-   这是一个 Scira 特有的扩展，标准的 OpenAI 客户端可能不会直接处理此字段。

### 7. 多用户 ID 轮询

为分散请求负载或使用不同的用户配额，Scira2API 支持配置多个 Scira 用户 ID。

-   通过 `USERIDS` 环境变量提供一个逗号分隔的用户 ID 列表。
-   对于每次向 Scira 服务发起的请求，服务会从这个列表中轮流选择一个 `userId` 使用。
-   如果 `USERIDS` 未配置或为空，服务会使用一个内部定义的默认 `userId`。

## 📜 日志与监控

### 日志系统

-   Scira2API 使用自定义的日志包 (`log/logger.go`)。
-   **输出**: 日志默认输出到标准控制台 (`os.Stdout`)。在 Docker 环境下，这些日志会被 Docker 守护进程捕获。
-   **格式**: `[YYYY-MM-DD HH:MM:SS.mmm] [LEVEL] Log message content`
-   **级别**: 支持 `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`。
    -   可通过 `LOG_LEVEL` 环境变量设置，默认为 `INFO`。
    -   设置为 `DEBUG` 可以获取更详细的内部处理信息，有助于故障排查。
-   **颜色**: 在支持彩色的终端上，不同级别的日志会以不同颜色显示，增强可读性。
-   **请求追踪**: 每个进入的 API 请求会被分配一个唯一的请求 ID (例如 `req_xxxxxxxx`)，该 ID 会出现在相关的日志条目中，便于追踪单个请求的处理全过程。

### 健康检查与性能指标

-   **`/health`**: 提供服务基本健康状态，详见 [API 接口文档](#1-健康检查)。
-   **`/metrics`**: 提供详细的运行时性能指标，兼容 Prometheus 格式，详见 [API 接口文档](#2-性能指标)。
-   **Docker 健康检查**: `Dockerfile` 和 `docker-compose.yml` 都配置了健康检查，通常指向 `/v1/models` 或 `/health` 端点。

## 🏗️ 系统架构与设计模式

### 高级架构

Scira2API 采用典型的分层架构：

1.  **API 层 (Interface/Transport Layer)**:
    -   基于 `gin-gonic/gin` Web 框架。
    -   负责处理 HTTP 请求的路由、解析、参数绑定和响应序列化。
    -   包含各种中间件 (认证、CORS、错误处理、请求统计)。
2.  **服务层 (Service/Business Logic Layer)**:
    -   核心业务逻辑的实现，如 `service.ChatHandler`。
    -   处理 OpenAI API 到 Scira 服务请求的适配转换。
    -   管理 Tokens 计算、缓存、代理、连接池、速率限制等高级功能。
3.  **客户端层 (Integration/Client Layer)**:
    -   封装了与下游 Scira 服务进行 HTTP(S) 通信的逻辑 (`pkg/http.HttpClient`)。
    -   包含重试机制、超时控制和代理集成。

### 使用的设计模式 (部分)

-   **适配器 (Adapter)**: 核心模式，用于转换 OpenAI API 格式与 Scira 服务特定格式之间的请求和响应。
-   **中间件 (Middleware)**: Gin 框架广泛使用，用于解耦横切关注点，如认证、日志、CORS、错误处理。
-   **构建器 (Builder)**: 如 `service.ChatHandlerBuilder`，用于复杂对象的逐步构建和配置。
-   **单例 (Singleton)**: 用于管理全局唯一的资源，如配置对象、日志器、缓存实例等 (通过 `sync.Once` 或包级变量实现)。
-   **策略 (Strategy)**: 例如根据配置启用或禁用缓存、代理等不同行为。
-   **依赖注入 (Dependency Injection)**: 通过构造函数或初始化方法传入依赖项，降低耦合。

### 技术栈

-   **后端语言**: Go (版本 >= 1.23.0, 工具链 go1.24.3)
-   **Web 框架**: Gin (`github.com/gin-gonic/gin`)
-   **配置加载**: `github.com/joho/godotenv` (用于 `.env` 文件), 标准库 `os` (环境变量)
-   **HTTP 客户端**: 自定义 `pkg/http.HttpClient` (可能基于或增强了 `go-resty/resty/v2` 或标准库 `net/http`)，集成了连接池和代理。
-   **并发处理**: Go 协程 (goroutines), 通道 (channels), `sync` 包 (Mutex, WaitGroup, Once)
-   **JSON 处理**: 标准库 `encoding/json`
-   **日志**: 自定义日志包，使用 `github.com/fatih/color` 实现控制台颜色。
-   **容器化**: Docker, Docker Compose
-   **依赖管理**: Go Modules (`go.mod`, `go.sum`)

## 🧩 项目结构

```
scira2api/
├── .env.example           # 环境变量配置文件模板
├── Dockerfile             # Docker 镜像构建文件
├── docker-compose.yml     # Docker Compose 编排文件
├── go.mod                 # Go 模块依赖管理
├── go.sum                 # Go 模块校验和
├── LICENSE                # 项目许可证
├── main.go                # 应用主入口
├── README.md              # 项目说明文档 (本文档)
├── config/                # 配置加载与管理
│   ├── config.go          # 配置结构体定义和加载逻辑
│   └── model_mapping.go   # 硬编码的默认模型映射
├── log/                   # 自定义日志组件
│   └── logger.go
├── middleware/            # Gin 中间件
│   ├── auth.go            # API 密钥认证中间件
│   ├── cors.go            # CORS 处理中间件
│   ├── error.go           # 全局错误处理中间件
│   └── response.go        # 标准化响应辅助函数 (含错误响应)
├── models/                # API 请求/响应及内部数据结构定义
│   └── models.go
├── pkg/                   # 可复用的公共包/工具库
│   ├── cache/             # 响应缓存实现
│   ├── connpool/          # HTTP 连接池实现
│   ├── constants/         # 项目中使用的常量
│   ├── errors/            # 自定义错误类型
│   ├── http/              # 自定义 HTTP 客户端
│   ├── manager/           # 用户 ID 管理、会话 ID 生成
│   └── ratelimit/         # 速率限制实现
├── proxy/                 # 代理管理相关逻辑
│   └── proxy_manager.go   # 动态代理管理器接口/实现框架
└── service/               # 核心业务逻辑服务
    ├── chat.go            # /v1/chat/completions 非流式处理
    ├── handler.go         # ChatHandler 定义、构建器及依赖注入
    ├── interfaces.go      # 服务层接口定义
    ├── model.go           # /v1/models 处理逻辑
    ├── stream.go          # /v1/chat/completions 流式 (SSE) 处理
    ├── stream_helpers.go  # 流式处理辅助函数
    ├── token_counter.go   # 请求级 Token 计数器
    ├── utils.go           # 服务层工具函数 (Token 计算, Scira 响应解析等)
    └── validator.go       # 请求参数校验逻辑
```

## ❓ 故障排除与常见问题

### 常见问题

1.  **服务启动失败**
    -   **检查日志**: 使用 `docker-compose logs -f scira2api` (Docker) 或查看控制台输出 (本地) 获取详细错误信息。
    -   **检查 `.env` 文件**: 确保 `.env` 文件存在于项目根目录，并且所有必需的环境变量 (特别是 `USERIDS`) 已正确配置。
    -   **端口冲突**: 确保配置的 `PORT` 没有被其他应用占用。

2.  **认证失败 (HTTP 401)**
    -   **检查 `APIKEY`**: 确保 `APIKEY` 环境变量已在服务端正确设置。
    -   **检查请求头**: 客户端请求的 `Authorization` 头部必须为 `Bearer YOUR_API_KEY` 格式，且 `YOUR_API_KEY` 与服务端配置一致。
    -   **公共路径**: 确认您访问的路径是否确实需要认证 (例如 `/health`, `/v1/models` 通常不需要)。

3.  **模型不可用或找不到 (HTTP 400/404)**
    -   **检查请求中的模型名称**: 确保您请求的 `model` ID 与 `/v1/models` 接口返回的列表中的某个 `id` 完全一致。
    -   **模型映射**: 参考[配置选项](#%EF%B8%8F-配置选项)中关于 `MODEL_MAPPING` 的说明。默认情况下，模型列表由 `config/model_mapping.go` 硬编码。

4.  **请求超时**
    -   **增加客户端超时**: 调整 `CLIENT_TIMEOUT` 环境变量以允许更长的 Scira 服务响应时间。
    -   **检查网络**: 检查服务器与 Scira 服务之间的网络连接。
    -   **代理配置**: 如果使用代理，请确保代理服务器工作正常且配置正确 (`HTTP_PROXY`, `SOCKS5_PROXY`)。
    -   **Scira 服务本身**: 问题可能源于 Scira 服务响应缓慢。

5.  **流式响应问题**
    -   **客户端实现**: 确保您的客户端库能够正确处理 Server-Sent Events (SSE) 协议，包括解析 `data:` 行和 `[DONE]` 结束标志。
    -   **`stream: true`**: 确保请求体中 `stream` 参数设置为 `true`。
    -   **网络稳定性**: 不稳定的网络可能导致 SSE 连接中断。服务内置了心跳机制，但极端情况下仍可能受影响。

### 启用调试模式

要获取更详细的日志以帮助诊断问题，可以将日志级别设置为 `debug`：

在您的 `.env` 文件中添加或修改：
```env
LOG_LEVEL=debug
```
然后重启服务。

## 🤝 贡献

欢迎各种形式的贡献，包括但不限于：
-   报告 Bug
-   提交功能需求
-   编写和改进文档
-   提交 Pull Requests

请确保您的代码贡献遵循项目的编码风格和测试要求。

## 📝 许可证

本项目基于 [LICENSE](LICENSE) 文件中规定的许可证 (例如 MIT, Apache 2.0 等，请用户根据实际情况填写) 开源。