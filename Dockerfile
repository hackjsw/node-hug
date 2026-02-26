# ==========================================
# 第一阶段：编译环境 (Builder)
# ==========================================
FROM golang:1.21-alpine AS builder

WORKDIR /app

# 【关键修改】把 main.go 和 index.html 一起复制到编译环境里
COPY main.go index.html ./

# 自动初始化并下载依赖
RUN go mod init proxy-node && \
    go get github.com/gorilla/websocket && \
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

# 从第一阶段把编译好的程序拿过来
COPY --from=builder /app/server .

# （注意：如果你在 main.go 里用了 go:embed，其实这里都不需要再复制 index.html 了，因为它已经被揉进 server 文件里了。但为了保险起见，我们还是带着它）
COPY index.html .

EXPOSE 3000

# 启动程序
RUN chmod +x server
CMD ["./server"]
