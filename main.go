package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ======================= 基础设定区 =======================
var (
	AppKey       = getEnv("UUID", "c80d8f80-7148-4764-8c64-5e629a47bee9")
	ServDomain   = getEnv("DOMAIN", "1234.abc.com")
	AutoTask     = getEnvBool("AUTO_ACCESS", false)
	RoutePath    = getEnv("WSPATH", AppKey[:8])
	DataPath     = getEnv("SUB_PATH", "lei")
	WorkerId     = getEnv("NAME", "hfgo")
	ListenPort   = getEnv("PORT", "3000")

	// 内部运行状态变量
	envMeta      string
	envMutex     sync.RWMutex
	keyBytes     []byte
	secretHash   string
)

func init() {
	// 预处理验证凭证
	cleanToken := strings.ReplaceAll(AppKey, "-", "")
	var err error
	keyBytes, err = hex.DecodeString(cleanToken)
	if err != nil || len(keyBytes) != 16 {
		log.Fatalf("Invalid token format: %v", err)
	}

	// 预计算二级验证哈希
	h := sha256.New224()
	h.Write([]byte(AppKey))
	secretHash = hex.EncodeToString(h.Sum(nil))
}
// ======================================================

func main() {
	// 1. 初始化环境元数据
	go updateEnvMeta()

	// 2. 挂载基础路由
	http.HandleFunc("/", baseHandler)

	// 3. 启动后台守护任务
	go startRoutineJob()

	// 4. 启动主服务进程
	log.Printf("Service worker is running on %s", ListenPort)
	if err := http.ListenAndServe(":"+ListenPort, nil); err != nil {
		log.Fatal("Service failed:", err)
	}
}

func baseHandler(w http.ResponseWriter, r *http.Request) {
	// 识别并处理数据流升级请求
	if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		processDataStream(w, r)
		return
	}

	// 处理常规访问请求
	if r.URL.Path == "/" {
		filePath := filepath.Join(".", "index.html")
		content, err := os.ReadFile(filePath)
		if err != nil {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Service Worker Active"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write(content)

	} else if r.URL.Path == "/"+DataPath {
		envMutex.RLock()
		currentEnv := envMeta
		envMutex.RUnlock()

		nodeTag := currentEnv
		if WorkerId != "" {
			nodeTag = fmt.Sprintf("%s-%s", WorkerId, currentEnv)
		}

		// 生成节点配置信息 (脱敏拼接)
		linkAlpha := fmt.Sprintf("vless://%s@cdns.doon.eu.org:443?encryption=none&security=tls&sni=%s&fp=chrome&type=ws&host=%s&path=%%2F%s#%s",
			AppKey, ServDomain, ServDomain, RoutePath, nodeTag)
		linkBeta := fmt.Sprintf("trojan://%s@cdns.doon.eu.org:443?security=tls&sni=%s&fp=chrome&type=ws&host=%s&path=%%2F%s#%s",
			AppKey, ServDomain, ServDomain, RoutePath, nodeTag)

		nodeProfile := linkAlpha + "\n" + linkBeta
		base64Content := base64.StdEncoding.EncodeToString([]byte(nodeProfile))

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(base64Content + "\n"))
	} else {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Resource Not Found\n"))
	}
}

var streamUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func processDataStream(w http.ResponseWriter, r *http.Request) {
	conn, err := streamUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	adapter := &streamAdapter{Conn: conn}

	// 读取首个数据块进行协议特征识别
	_, buffer, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	// 识别是否为 Alpha 协议特征
	if len(buffer) > 17 && buffer[0] == 0 {
		if bytes.Equal(buffer[1:17], keyBytes) {
			if !handleProtocolAlpha(adapter, buffer) {
				conn.Close()
			}
			return
		}
	}

	// 否则尝试走 Beta 协议逻辑
	if !handleProtocolBeta(adapter, buffer) {
		conn.Close()
	}
}

func handleProtocolAlpha(adapter *streamAdapter, buffer []byte) bool {
	version := buffer[0]
	optLen := int(buffer[17])
	i := 18 + optLen

	if i+4 > len(buffer) {
		return false
	}

	targetIdx := binary.BigEndian.Uint16(buffer[i : i+2])
	i += 2
	atyp := buffer[i]
	i += 1

	var targetNode string
	switch atyp {
	case 1:
		if i+4 > len(buffer) { return false }
		targetNode = net.IP(buffer[i : i+4]).String()
		i += 4
	case 2:
		if i+1 > len(buffer) { return false }
		nodeLen := int(buffer[i])
		i += 1
		if i+nodeLen > len(buffer) { return false }
		targetNode = string(buffer[i : i+nodeLen])
		i += nodeLen
	case 3:
		if i+16 > len(buffer) { return false }
		targetNode = net.IP(buffer[i : i+16]).String()
		i += 16
	default:
		return false
	}

	payload := buffer[i:]

	// 响应握手信号
	adapter.Write([]byte{version, 0})

	resolvedNode := lookupResource(targetNode)
	backend, err := net.Dial("tcp", fmt.Sprintf("%s:%d", resolvedNode, targetIdx))
	if err != nil {
		backend, err = net.Dial("tcp", fmt.Sprintf("%s:%d", targetNode, targetIdx))
		if err != nil {
			return false
		}
	}

	if len(payload) > 0 {
		backend.Write(payload)
	}

	go relayData(adapter, backend)
	return true
}

func handleProtocolBeta(adapter *streamAdapter, buffer []byte) bool {
	if len(buffer) < 58 {
		return false
	}

	receivedAuth := string(buffer[:56])
	if receivedAuth != secretHash {
		return false
	}

	offset := 56
	if offset+1 < len(buffer) && buffer[offset] == '\r' && buffer[offset+1] == '\n' {
		offset += 2
	}

	if offset >= len(buffer) || buffer[offset] != 0x01 {
		return false
	}
	offset++

	if offset >= len(buffer) { return false }
	atyp := buffer[offset]
	offset++

	var targetNode string
	switch atyp {
	case 1:
		if offset+4 > len(buffer) { return false }
		targetNode = net.IP(buffer[offset : offset+4]).String()
		offset += 4
	case 3:
		if offset >= len(buffer) { return false }
		nodeLen := int(buffer[offset])
		offset++
		if offset+nodeLen > len(buffer) { return false }
		targetNode = string(buffer[offset : offset+nodeLen])
		offset += nodeLen
	case 4:
		if offset+16 > len(buffer) { return false }
		targetNode = net.IP(buffer[offset : offset+16]).String()
		offset += 16
	default:
		return false
	}

	if offset+2 > len(buffer) { return false }
	targetIdx := binary.BigEndian.Uint16(buffer[offset : offset+2])
	offset += 2

	if offset+1 < len(buffer) && buffer[offset] == '\r' && buffer[offset+1] == '\n' {
		offset += 2
	}

	payload := buffer[offset:]

	resolvedNode := lookupResource(targetNode)
	backend, err := net.Dial("tcp", fmt.Sprintf("%s:%d", resolvedNode, targetIdx))
	if err != nil {
		backend, err = net.Dial("tcp", fmt.Sprintf("%s:%d", targetNode, targetIdx))
		if err != nil {
			return false
		}
	}

	if len(payload) > 0 {
		backend.Write(payload)
	}

	go relayData(adapter, backend)
	return true
}

// ======================= 辅助工具集 =======================

func lookupResource(resource string) string {
	if net.ParseIP(resource) != nil {
		return resource
	}
	
	queryUri := "https://dns.google/resolve?name=" + url.QueryEscape(resource) + "&type=A"
	req, _ := http.NewRequest("GET", queryUri, nil)
	req.Header.Set("Accept", "application/dns-json")
	client := &http.Client{Timeout: 5 * time.Second}
	
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		var data struct {
			Status int `json:"Status"`
			Answer []struct {
				Type int    `json:"type"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		if json.NewDecoder(resp.Body).Decode(&data) == nil && data.Status == 0 {
			for _, ans := range data.Answer {
				if ans.Type == 1 {
					return ans.Data
				}
			}
		}
	}
	return resource
}

func updateEnvMeta() {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.ip.sb/geoip")
	if err != nil {
		setEnvMeta("Default_Env")
		return
	}
	defer resp.Body.Close()

	var data struct {
		CountryCode string `json:"country_code"`
		ISP         string `json:"isp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		setEnvMeta("Default_Env")
		return
	}

	meta := fmt.Sprintf("%s-%s", data.CountryCode, data.ISP)
	meta = strings.ReplaceAll(meta, " ", "_")
	setEnvMeta(meta)
}

func setEnvMeta(val string) {
	envMutex.Lock()
	defer envMutex.Unlock()
	envMeta = val
}

func startRoutineJob() {
	if !AutoTask || ServDomain == "" {
		return
	}
	fullLink := fmt.Sprintf("https://%s/%s", ServDomain, DataPath)
	payload, _ := json.Marshal(map[string]string{"url": fullLink})

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("POST", "https://oooo.serv00.net/add-url", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		log.Println("Routine job triggered successfully")
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		return strings.ToLower(value) == "true" || value == "1"
	}
	return fallback
}

// ======================= 数据流转换器 =======================

type streamAdapter struct {
	*websocket.Conn
	reader io.Reader
}

func (c *streamAdapter) Read(p []byte) (n int, err error) {
	for {
		if c.reader == nil {
			mt, r, err := c.NextReader()
			if err != nil {
				return 0, err
			}
			if mt != websocket.BinaryMessage {
				continue
			}
			c.reader = r
		}
		n, err = c.reader.Read(p)
		if err == io.EOF {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *streamAdapter) Write(p []byte) (n int, err error) {
	err = c.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// 核心数据转发桥接
func relayData(src io.ReadWriteCloser, dst net.Conn) {
	defer src.Close()
	defer dst.Close()

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(src, dst)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(dst, src)
		errc <- err
	}()
	<-errc
}
