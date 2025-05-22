# Scira2api

将 [Scira 的网页服务](https://mcp.scira.ai/) 转换为 API 服务，支持 OpenAI 格式的访问。

## ✨ 特性

- 🔁 **UserId 轮询** - 支持多个 userIds 的轮询机制
- 📝 **自动会话管理** - 使用后可自动删除会话
- 🌊 **流式响应** - 获取实时流式输出
- 🌐 **代理支持** - 通过您首选的代理路由请求
- 🔐 **API 密钥认证** - 保护您的 API 端点
- 🔁 **自动重试** - 请求失败时自动重试
- 🚀 **高性能 HTTP 客户端** - 使用 go-resty 库实现高效的 HTTP 请求

## 📋 先决条件

- Go 1.24+ (从源代码构建)
- Docker (容器化部署)

## 🚀 部署选项

### Docker

```bash
docker run -d \
  -p 8080:8080 \
  -e USERIDS=xxx,yyy \
  -e APIKEY=sk-123 \
  -e CHAT_DELETE=true \
  -e HTTP_PROXY=http://127.0.0.1:7890 \
  -e MODELS=gpt-4.1-mini,claude-3-7-sonnet,grok-3-mini,qwen-qwq \
  -e RETRY=3 \
  --name scira2api \
  ghcr.io/coderzoe/scira2api:latest
```

### Docker Compose

创建 `docker-compose.yml` 文件:

```yaml
version: '3'
services:
  scira2api:
    image: ghcr.io/coderzoe/scira2api:latest
    container_name: scira2api
    ports:
      - "8080:8080"
    environment:
      - USERIDS=xxx,yyy  # 必需
      - APIKEY=sk-123  # 可选
      - CHAT_DELETE=true  # 可选
      - HTTP_PROXY=http://127.0.0.1:7890  # 可选
      - MODELS=gpt-4.1-mini,claude-3-7-sonnet,grok-3-mini,qwen-qwq   # 可选
      - RETRY=3  # 可选
    restart: unless-stopped
```

然后运行:

```bash
docker-compose up -d
```

或者:

```bash
# 克隆仓库
git clone https://github.com/coderZoe/scira2api.git
cd scira2api
# 编辑环境变量
vi docker-compose.yml
./deploy.sh
```

### 直接部署

```bash
# 克隆仓库
git clone https://github.com/coderZoe/scira2api.git
cd scira2api
cp .env.example .env  
vim .env  
# 构建二进制文件
go build -o scira2api .

./scira2api
```

## ⚙️ 配置

### 环境变量配置

您可以使用应用程序根目录中的 `.env` 文件配置 `scira2api`。如果此文件存在，将优先使用它而不是环境变量。

示例 `.env`:

```yaml
# 必需，使用英文逗号分隔多个 userIds
USERIDS= xxx,yyy

# 可选，端口。默认: 8080
PORT=8080

# 可选，用于验证客户端请求的 API 密钥（例如，为 openweb-ui 请求输入的密钥）。如果为空，则不需要认证。
APIKEY=sk-xxx

# 可选，代理地址。默认: 不使用代理。
HTTP_PROXY= http://127.0.0.1:7890

# 可选，模型列表，用英文逗号分隔。
MODELS=gpt-4.1-mini,claude-3-7-sonnet,grok-3-mini,qwen-qwq

# 可选，请求失败时的重试次数。0 或 1 表示不重试。默认: 0（不重试）。每次重试将使用不同的 userId。
RETRY=3

# 可选，是否删除页面上的聊天历史。默认: false（不删除）。
CHAT_DELETE=true
```

仓库中提供了一个示例配置文件 `.env.example`。

## 📝 API 使用

### 认证

在请求头中包含您的 API 密钥:

```bash
# 如果未配置 apiKey，则不需要
Authorization: Bearer YOUR_API_KEY
```

### 聊天补全

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "gpt-4.1-mini",
    "messages": [
      {
        "role": "user",
        "content": "你好，请问你是谁？"
      }
    ],
    "stream": true
  }'
```

## 🛠️ 技术实现

本项目使用 [go-resty](https://github.com/go-resty/resty) 库作为 HTTP 客户端，它是一个简单而强大的 Go HTTP 客户端库，提供以下优势：

- 简洁的 API 设计
- 支持中间件和拦截器
- 内置重试机制
- 高效的流式处理
- 广泛的社区支持和维护

## 🤝 贡献

欢迎贡献！请随时提交 Pull Request。

1. Fork 仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交您的更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 打开 Pull Request

## 📄 许可证

本项目采用 MIT 许可证 - 详情请参阅 [LICENSE](LICENSE) 文件。

---

由 [coderZoe](https://github.com/coderZoe) 用 ❤️ 制作