# ==========================================
# 第一阶段：编译环境 (Builder)
# ==========================================
FROM golang:1.21-alpine AS builder

WORKDIR /tmp

# 把当前 GitHub 仓库里的所有文件（主要是 main.go）复制到容器内
COPY . .

# 告诉系统自动初始化 Go 模块并下载依赖包（替代了 npm install）
RUN go mod init proxy-node && \
    go get nhooyr.io/websocket && \
    go mod tidy

# 编译 Go 代码，生成名为 "server" 的二进制可执行文件
RUN go build -o server main.go

# ==========================================
# 第二阶段：运行环境 (Runner)
# ==========================================
FROM alpine:3.20

WORKDIR /tmp

# 安装基础的网络证书（Go 发起 HTTPS 请求获取 ISP 时需要）和调试工具
RUN apk update && apk add --no-cache ca-certificates bash curl tzdata

# 从第一阶段把编译好的 "server" 文件拿过来，其余的源码和 Go 环境全丢弃
COPY --from=builder /tmp/server .

# 关键修改：为 HF 创建特定用户并赋予权限
RUN adduser -D -u 1000 myuser
RUN chown -R myuser:myuser /app && chmod +x /app/server
USER 1000

EXPOSE 7860

CMD ["./server"]
