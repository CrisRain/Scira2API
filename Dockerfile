# 多阶段构建 - 构建阶段
FROM golang:1.24-alpine AS builder

# 设置Go环境变量 - 确保使用模块模式
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org

# 安装构建依赖
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata

# 设置工作目录
WORKDIR /build

# 复制依赖文件并下载依赖（利用Docker缓存层）
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# 复制源代码
COPY . .

# 整理模块依赖
RUN go mod tidy
# 验证模块状态并构建应用
RUN echo "=== Module Information ===" && \
    go version && \
    go env GO111MODULE && \
    go env GOPATH && \
    go env GOROOT && \
    go list -m && \
    echo "=== Building Application ===" && \
    go build \
    -v \
    -a \
    -installsuffix cgo \
    -ldflags="-w -s" \
    -o scira2api \
    .

# 运行阶段 - 使用更小的基础镜像
FROM alpine:3.19

# 安装运行时依赖
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    && update-ca-certificates

# 设置时区
ENV TZ=Asia/Shanghai

# 创建应用目录和非root用户
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# 创建必要的目录
RUN mkdir -p /app/log && \
    chown -R appuser:appgroup /app

# 设置工作目录
WORKDIR /app

# 从构建阶段复制应用
COPY --from=builder --chown=appuser:appgroup /build/scira2api .

# 复制配置文件（如果存在）
# COPY --from=builder --chown=appuser:appgroup /build/.env.example .env.example

# 切换到非root用户
USER appuser

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/v1/models || exit 1

# 声明端口
EXPOSE 8080

# 设置启动命令
ENTRYPOINT ["./scira2api"] 