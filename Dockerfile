# ==========================================
# 第一阶段：编译环境 (Builder)
# ==========================================
FROM golang:1.21-alpine AS builder

WORKDIR /app

# 【精准复制】只把我们要编译的 Go 源文件复制进来
COPY main.go .

# 自动初始化并下载依赖
RUN go mod init proxy-node && \
    go get nhooyr.io/websocket && \
    go mod tidy

# 编译成 server 二进制文件
RUN go build -o server main.go

# ==========================================
# 第二阶段：运行环境 (Runner)
# ==========================================
FROM alpine:3.20

WORKDIR /app

# 安装基础的网络请求证书和调试工具
RUN apk update && apk add --no-cache ca-certificates bash curl tzdata

# 【精准复制 1】从第一阶段把编译好的程序拿过来
COPY --from=builder /app/server .

# 【精准复制 2】从你的 GitHub 仓库里把网页文件拿过来
COPY index.html .

EXPOSE 3000

# 启动程序
RUN chmod +x server
CMD ["./server"]
