package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"nhooyr.io/websocket"
)

// --- 1. 配置项初始化 ---
var (
	UUID     = getEnv("UUID", "5efabea4-f6d4-91fd-b8f0-17e004c89c60")
	DOMAIN   = getEnv("DOMAIN", "1234.abc.com")
	WSPATH   = getEnv("WSPATH", UUID[:8])
	SUB_PATH = getEnv("SUB_PATH", "sub")
	NAME     = getEnv("NAME", "")
	PORT     = getEnv("PORT", "3000")
	ISP      = "Unknown"
)

// 辅助函数：获取环境变量，带默认值
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}

// --- 2. 获取 ISP 信息 ---
type IPAPIResponse struct {
	CountryCode string `json:"country_code"`
	ISP         string `json:"isp"`
}

func fetchISP() {
	resp, err := http.Get("https://api.ip.sb/geoip")
	if err != nil {
		log.Println("获取 ISP 失败:", err)
		return
	}
	defer resp.Body.Close()

	var data IPAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
		ISP = strings.ReplaceAll(fmt.Sprintf("%s-%s", data.CountryCode, data.ISP), " ", "_")
		log.Println("当前 ISP 信息:", ISP)
	}
}

// --- 3. 核心业务逻辑 ---
func main() {
	fetchISP()

	// 路由分发
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/"+SUB_PATH, handleSub)
	http.HandleFunc("/"+WSPATH, handleWebSocket) // 处理代理流量的专用路径

	log.Printf("纯净版服务端已启动，监听端口: %s", PORT)
	if err := http.ListenAndServe(":"+PORT, nil); err != nil {
		log.Fatal("服务异常退出:", err)
	}
}

// 根目录：返回 index.html 或打招呼
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	htmlPath := filepath.Join(".", "index.html")
	if content, err := os.ReadFile(htmlPath); err == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(content)
	} else {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello from Go Server!"))
	}
}

// 订阅路由：生成 Base64 订阅链接
func handleSub(w http.ResponseWriter, r *http.Request) {
	namePart := ISP
	if NAME != "" {
		namePart = fmt.Sprintf("%s-%s", NAME, ISP)
	}

	vlessURL := fmt.Sprintf("vless://%s@cdns.doon.eu.org:443?encryption=none&security=tls&sni=%s&fp=chrome&type=ws&host=%s&path=%%2F%s#%s",
		UUID, DOMAIN, DOMAIN, WSPATH, namePart)
	
	trojanURL := fmt.Sprintf("trojan://%s@cdns.doon.eu.org:443?security=tls&sni=%s&fp=chrome&type=ws&host=%s&path=%%2F%s#%s",
		UUID, DOMAIN, DOMAIN, WSPATH, namePart)

	subscription := vlessURL + "\n" + trojanURL
	base64Content := base64.StdEncoding.EncodeToString([]byte(subscription))

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(base64Content + "\n"))
}

// --- 4. WebSocket 隧道代理 (替换 JS 的 Wss Connection) ---
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 将普通 HTTP 请求升级为 WebSocket
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // 允许跨域等
	})
	if err != nil {
		return
	}
	defer wsConn.Close(websocket.StatusInternalError, "关闭连接")

	// 将 WebSocket 包装成标准的 net.Conn 流，这就是 Go 处理网络转发最舒服的地方
	clientConn := websocket.NetConn(context.Background(), wsConn, websocket.MessageBinary)
	defer clientConn.Close()

	// 读取客户端发来的第一段数据 (判断协议并提取目标地址)
	// TODO: 这里我们需要参考 JS 中的 handleVlessConnection 和 handleTrojanConnection 
	// 解析头部字节，拿到真实要访问的 targetHost 和 targetPort

	/* // 伪代码流程：
	   1. 读取 clientConn 的头部数据 (判断是 VLESS 还是 Trojan)
	   2. 解析出目标地址，例如 targetAddr := "www.google.com:443"
	   3. targetConn, err := net.Dial("tcp", targetAddr)
	   4. 双向转发 (类似 JS 里的 .pipe)
	   
	   go io.Copy(targetConn, clientConn)
	   io.Copy(clientConn, targetConn)
	*/
}
