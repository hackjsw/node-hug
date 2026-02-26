# ==========================================
# 第一阶段：编译环境 (Builder)
# ==========================================
FROM golang:1.21-alpine AS builder

# 【统一使用 /app，避开 /tmp 的坑】
WORKDIR /app

# 把代码（包括 main.go 和 index.html）复制进来
COPY . .

# 【关键修改】使用 gorilla/websocket 与你的 main.go 保持一致
RUN go mod init proxy-node && \
    go get github.com/gorilla/websocket && \
    go mod tidy

# 编译生成名为 "server" 的二进制文件
RUN go build -o server main.go

# ==========================================
# 第二阶段：运行环境 (Runner)
# ==========================================
FROM alpine:3.20

# 【统一使用 /app】
WORKDIR /app

# 安装基础的网络证书和调试工具
RUN apk update && apk add --no-cache ca-certificates bash curl tzdata

# 从第一阶段把编译好的程序拷过来（注意路径是 /app/server）
COPY --from=builder /app/server .

# ==========================================
# 权限与端口配置 (针对 Hugging Face)
# ==========================================
# 创建 UID 1000 的普通用户
RUN adduser -D -u 1000 myuser
# 给 /app 目录赋权，并给 server 添加可执行权限
RUN chown -R myuser:myuser /app && chmod +x /app/server

# 切换到该用户
USER 1000

# 暴露 HF 规定的 7860 端口
EXPOSE 7860

# 启动程序
CMD ["./server"]
