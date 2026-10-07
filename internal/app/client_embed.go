package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// 同进程桌面外壳（Wails v3）接入点。外壳在调用 Run() 之前设置这些钩子；
// Electron 子进程模式下全部为空，行为与之前完全一致。

// ClientHost 由桌面外壳实现：后端就绪回调 + 原生能力（目录选择、在访达中显示、全屏、通知等）。
type ClientHost interface {
	// ClientReady 在 HTTP 服务开始监听后调用，url 已带一次性 token。
	ClientReady(port int, url string)
	// Native 处理 /api/desktop/native/<action>，body 为请求 JSON。
	Native(action string, body json.RawMessage) (any, error)
}

var (
	clientHostMu sync.RWMutex
	clientHost   ClientHost
	// ClientUIFS 非空时客户端模式直接从该文件系统提供前端（Wails 外壳把 dist 嵌入二进制）。
	ClientUIFS fs.FS
	clientStop = make(chan struct{})
	stopOnce   sync.Once

	currentClientMu sync.RWMutex
	currentClient   *UIApp

	eventsMu  sync.Mutex
	eventSubs = map[chan string]struct{}{}
)

// SetClientHost 注册桌面外壳。
func SetClientHost(host ClientHost) {
	clientHostMu.Lock()
	clientHost = host
	clientHostMu.Unlock()
}

func getClientHost() ClientHost {
	clientHostMu.RLock()
	defer clientHostMu.RUnlock()
	return clientHost
}

// StopClient 让 RunClient 优雅退出（外壳退出时调用）。
func StopClient() { stopOnce.Do(func() { close(clientStop) }) }

// ClientActivity 返回进行中的下载与合并数（退出确认、防休眠、托盘提示用）。
func ClientActivity() (downloads, merges int) {
	currentClientMu.RLock()
	a := currentClient
	currentClientMu.RUnlock()
	if a == nil {
		return 0, 0
	}
	return a.clientActivity()
}

func (a *UIApp) clientActivity() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	active := 0
	for _, id := range a.taskOrder {
		task := a.tasks[id]
		if task != nil && (task.Status == uiStatusRunning || task.Status == uiStatusQueued || task.Status == uiStatusParsing) {
			active++
		}
	}
	return active, len(a.mergeJobs)
}

// ClientEmit 向页面推送事件（SSE：/api/desktop/native/events），例如菜单触发的导航。
func ClientEmit(event string, data any) {
	payload, _ := json.Marshal(map[string]any{"event": event, "data": data})
	eventsMu.Lock()
	defer eventsMu.Unlock()
	for ch := range eventSubs {
		select {
		case ch <- string(payload):
		default:
		}
	}
}

// SafeGo 启动带 panic recover 的 goroutine：同进程外壳下单个后台任务崩溃不应带走整个应用。
func SafeGo(name string, fn func()) {
	go func() {
		defer recoverAndLog(name)
		fn()
	}()
}

func recoverAndLog(name string) {
	if value := recover(); value != nil {
		message := fmt.Sprintf("%s panic: %v", name, value)
		fmt.Fprintln(os.Stderr, message)
		currentClientMu.RLock()
		a := currentClient
		currentClientMu.RUnlock()
		if a != nil && a.downloader != nil {
			a.downloader.recordDiagnostic(diagnosticEvent{Level: "error", Event: "panic", Message: message})
		}
	}
}

// mediaToken 由启动令牌派生，只用于播放地址（原生 <video>/AVFoundation 请求可能带不上 Cookie）。
func mediaToken(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("bytetv-media"))
	return hex.EncodeToString(mac.Sum(nil))[:40]
}

// mediaPath 判断是否为允许用 ?bt= 凭据访问的只读媒体路径。
func mediaPath(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	p := request.URL.Path
	return strings.HasPrefix(p, "/api/ui/playback/") || strings.HasPrefix(p, "/api/ui/media") || strings.HasPrefix(p, "/api/ui/file") || strings.HasPrefix(p, "/api/ui/cover")
}

func clientPortFile(dataDir string) string { return filepath.Join(dataDir, "client_port") }

func rememberedClientPort(dataDir string) int {
	data, err := os.ReadFile(clientPortFile(dataDir))
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return port
}

func rememberClientPort(dataDir string, port int) {
	_ = os.WriteFile(clientPortFile(dataDir), []byte(strconv.Itoa(port)), 0o600)
}

// ---- 桌面偏好（播放偏好/主题/引导标记等）：存后端，避免端口变化导致 localStorage 丢失 ----

var prefsMu sync.Mutex

func desktopPrefsPath(dataDir string) string { return filepath.Join(dataDir, "desktop_prefs.json") }

func readDesktopPrefs(dataDir string) map[string]any {
	prefs := map[string]any{}
	if data, err := os.ReadFile(desktopPrefsPath(dataDir)); err == nil {
		_ = json.Unmarshal(data, &prefs)
	}
	return prefs
}

func (server *clientServer) handlePrefs(writer http.ResponseWriter, request *http.Request) {
	dataDir := server.ui.cfg.dataDirectory()
	prefsMu.Lock()
	defer prefsMu.Unlock()
	prefs := readDesktopPrefs(dataDir)
	if request.Method == http.MethodPost {
		var patch map[string]any
		if err := json.NewDecoder(io.LimitReader(request.Body, 1<<20)).Decode(&patch); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		for key, value := range patch {
			if value == nil {
				delete(prefs, key)
			} else {
				prefs[key] = value
			}
		}
		data, _ := json.MarshalIndent(prefs, "", "  ")
		if err := os.WriteFile(desktopPrefsPath(dataDir), data, 0o600); err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(writer, http.StatusOK, prefs)
}

func (server *clientServer) handleNative(writer http.ResponseWriter, request *http.Request) {
	action := strings.TrimPrefix(request.URL.Path, "/api/desktop/native/")
	if action == "events" {
		server.handleEvents(writer, request)
		return
	}
	host := getClientHost()
	if action == "info" {
		writeJSON(writer, http.StatusOK, map[string]any{"shell": host != nil, "platform": runtime.GOOS, "mediaToken": server.mediaToken})
		return
	}
	if host == nil {
		writeJSON(writer, http.StatusNotImplemented, map[string]string{"error": "当前不是桌面外壳"})
		return
	}
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if len(body) == 0 {
		body = []byte("null")
	}
	result, err := func() (result any, err error) {
		defer func() {
			if value := recover(); value != nil {
				err = fmt.Errorf("native %s panic: %v", action, value)
			}
		}()
		return host.Native(action, body)
	}()
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"result": result})
}

func (server *clientServer) handleEvents(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	ch := make(chan string, 16)
	eventsMu.Lock()
	eventSubs[ch] = struct{}{}
	eventsMu.Unlock()
	defer func() {
		eventsMu.Lock()
		delete(eventSubs, ch)
		eventsMu.Unlock()
	}()
	fmt.Fprint(writer, ": ok\n\n")
	flusher.Flush()
	for {
		select {
		case <-request.Context().Done():
			return
		case message := <-ch:
			fmt.Fprintf(writer, "data: %s\n\n", message)
			flusher.Flush()
		}
	}
}

// spaFSServer 从嵌入的文件系统提供前端；未知路径回落到 index.html。
func spaFSServer(files fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
		if name == "" || name == "index.html" {
			serveIndex(writer, request, files)
			return
		}
		if info, err := fs.Stat(files, name); err != nil || info.IsDir() {
			if errors.Is(err, fs.ErrNotExist) || err == nil {
				serveIndex(writer, request, files)
				return
			}
		}
		fileServer.ServeHTTP(writer, request)
	})
}

func serveIndex(writer http.ResponseWriter, _ *http.Request, files fs.FS) {
	data, err := fs.ReadFile(files, "index.html")
	if err != nil {
		http.Error(writer, "index.html missing", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write(data)
}
