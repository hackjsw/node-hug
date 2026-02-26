# ==========================================
# 第一阶段：编译环境 (Builder)
# ==========================================
FROM golang:1.21-alpine AS builder

# 【关键修改】不要使用 /tmp，改为 /app
WORKDIR /app

# 复制源码到容器内
COPY . .

# 初始化模块并下载依赖包
RUN go mod init proxy-node && \
    go get nhooyr.io/websocket && \
    go mod tidy

# 编译生成名为 server 的二进制文件
RUN go build -o server main.go

# ==========================================
# 第二阶段：运行环境 (Runner)
# ==========================================
FROM alpine:3.20

# 【关键修改】这里也保持一致，改为 /app
WORKDIR /app

# 安装基础证书和工具
RUN apk update && apk add --no-cache ca-certificates bash curl tzdata

# 从 builder 阶段把编译好的程序拷过来
COPY --from=builder /app/server .

EXPOSE 3000

# 启动程序
RUN chmod +x server
CMD ["./server"]
