# 使用官方 Golang 镜像作为构建环境
FROM golang:1.24-alpine AS builder

# 设置工作目录
WORKDIR /app

# 设置 Go 代理以加速下载
ENV GOPROXY=https://goproxy.cn,direct

# 复制 go.mod 和 go.sum 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用程序
# CGO_ENABLED=0 表示禁用 CGO，构建静态链接的可执行文件
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o paper-service ./cmd/server

# 使用轻量级的 Alpine 镜像作为运行环境
FROM alpine:latest

# 安装 curl 用于健康检查
RUN apk add --no-cache curl

# 设置工作目录
WORKDIR /app

# 复制构建好的可执行文件
COPY --from=builder /app/paper-service .

# 复制配置文件
COPY --from=builder /app/.env .

# 创建必要的目录
RUN mkdir -p /app/uploads /app/logs

# 暴露端口
EXPOSE 8002

# 运行应用程序
CMD ["./paper-service"]