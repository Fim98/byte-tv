package app

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// 客户端模式（Electron 壳）：Go 后端作为子进程运行，只监听 127.0.0.1 随机端口，
// 用启动令牌鉴权，本机使用者即“本机所有者”，不经过账号体系。

const (
	clientAppName     = "果果剧库"
	clientTokenCookie = "juku_client"
	clientTokenEnv    = "JUKU_CLIENT_TOKEN"
)

type clientOwnerKey struct{}

func withClientOwner(ctx context.Context) context.Context {
	return context.WithValue(ctx, clientOwnerKey{}, true)
}

func isClientOwner(ctx context.Context) bool {
	owner, _ := ctx.Value(clientOwnerKey{}).(bool)
	return owner
}

// clientDataDirectory 返回系统标准用户数据目录，例如
// macOS ~/Library/Application Support/果果剧库，Windows %AppData%\果果剧库。
func clientDataDirectory() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, clientAppName), nil
}

// clientDefaultOutputDirectory 返回“影片 / 视频”目录下的默认下载位置。
func clientDefaultOutputDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return defaultOutputDir()
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Movies", clientAppName)
	case "windows":
		return filepath.Join(home, "Videos", clientAppName)
	default:
		if videos := filepath.Join(home, "Videos"); directoryExists(videos) {
			return filepath.Join(videos, clientAppName)
		}
		return filepath.Join(home, clientAppName)
	}
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// prepareClientDirectories 创建并切换到用户数据目录，使相对路径（bin/ FFmpeg 等）都落在数据目录内。
// 返回数据目录与首次运行时应使用的默认下载目录（已有配置时为空，保留用户设置）。
func prepareClientDirectories(explicitDataDir string) (string, string, error) {
	directory := strings.TrimSpace(explicitDataDir)
	if directory == "" {
		var err error
		if directory, err = clientDataDirectory(); err != nil {
			return "", "", err
		}
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", err
	}
	if err := os.Chdir(directory); err != nil {
		return "", "", err
	}
	output := ""
	if _, err := os.Stat(runtimeSettingsPath(directory)); errors.Is(err, os.ErrNotExist) {
		output = clientDefaultOutputDirectory()
	}
	return directory, output, nil
}

type clientServer struct {
	ui    *UIApp
	token string
	uiDir string
	port  int
}

func (server *clientServer) authorized(request *http.Request) bool {
	candidates := []string{request.Header.Get("X-Juku-Token")}
	if cookie, err := request.Cookie(clientTokenCookie); err == nil {
		candidates = append(candidates, cookie.Value)
	}
	for _, candidate := range candidates {
		if candidate != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(server.token)) == 1 {
			return true
		}
	}
	return false
}

func (server *clientServer) hostAllowed(host string) bool {
	// 防 DNS 重绑定：只接受 127.0.0.1:端口 / localhost:端口。
	expected := fmt.Sprintf(":%d", server.port)
	return host == "127.0.0.1"+expected || host == "localhost"+expected
}

func (server *clientServer) handler() http.Handler {
	api := server.ui.routes()
	var static http.Handler
	if server.uiDir != "" {
		static = spaFileServer(server.uiDir)
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !server.hostAllowed(request.Host) {
			http.Error(writer, "forbidden host", http.StatusForbidden)
			return
		}
		// 首次加载：Electron 打开 /?token=...，换成 HttpOnly Cookie 后跳回干净地址。
		if token := request.URL.Query().Get("token"); token != "" && request.Method == http.MethodGet && !strings.HasPrefix(request.URL.Path, "/api/") {
			if subtle.ConstantTimeCompare([]byte(token), []byte(server.token)) != 1 {
				http.Error(writer, "invalid token", http.StatusForbidden)
				return
			}
			http.SetCookie(writer, &http.Cookie{Name: clientTokenCookie, Value: server.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(writer, request, "/", http.StatusFound)
			return
		}
		if !server.authorized(request) {
			writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "客户端令牌无效，请从果果剧库应用内打开"})
			return
		}
		if request.URL.Path == "/api/desktop/health" {
			writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "pid": os.Getpid()})
			return
		}
		if static != nil && !strings.HasPrefix(request.URL.Path, "/api/") {
			static.ServeHTTP(writer, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/") {
			server.ui.attachDesktopViewer(writer, request)
		}
		api.ServeHTTP(writer, request.WithContext(withClientOwner(request.Context())))
	})
}

func spaFileServer(directory string) http.Handler {
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		clean := filepath.Join(directory, filepath.FromSlash(filepath.Clean("/"+request.URL.Path)))
		if info, err := os.Stat(clean); err != nil || info.IsDir() && request.URL.Path != "/" {
			writer.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(writer, request, filepath.Join(directory, "index.html"))
			return
		}
		if request.URL.Path == "/" || strings.HasSuffix(request.URL.Path, ".html") {
			writer.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(writer, request)
	})
}

// RunClient 以客户端模式提供服务（仅 127.0.0.1）。stdout 输出一行 JUKU_CLIENT_READY 供 Electron 解析端口。
// 标准输入关闭（父进程退出/崩溃）或收到 SIGTERM 时优雅退出，避免孤儿进程。
func (a *UIApp) RunClient(uiDir string, watchStdin bool, preferredPort int) error {
	token := strings.TrimSpace(os.Getenv(clientTokenEnv))
	if len(token) < 32 {
		return fmt.Errorf("客户端模式需要环境变量 %s（至少 32 个字符）", clientTokenEnv)
	}
	_ = os.Unsetenv(clientTokenEnv)
	a.clientMode = true
	a.enableWindowPlayback()
	if err := a.prepareBrowserViewers(); err != nil {
		return err
	}
	installer := a.downloader.ffmpegInstallation()
	go func() {
		path, err := installer.ensure(context.Background())
		if err != nil {
			fmt.Printf("FFmpeg 准备失败：%v\n", publicError(err))
			return
		}
		fmt.Printf("FFmpeg 已就绪：%s\n", path)
	}()
	// 优先复用上次端口，保证页面 origin 稳定（localStorage 等按 origin 保存）；被占用则随机端口。
	var listener net.Listener
	var err error
	if preferredPort > 1024 && preferredPort < 65536 {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	}
	if listener == nil {
		if listener, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if uiDir != "" {
		if abs, absErr := filepath.Abs(uiDir); absErr == nil {
			uiDir = abs
		}
		if _, statErr := os.Stat(filepath.Join(uiDir, "index.html")); statErr != nil {
			fmt.Printf("前端目录无效，忽略：%s\n", uiDir)
			uiDir = ""
		}
	}
	server := &clientServer{ui: a, token: token, uiDir: uiDir, port: port}
	a.mu.Lock()
	a.address = listener.Addr().String()
	a.mu.Unlock()
	httpServer := &http.Server{Handler: server.handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	fmt.Printf("JUKU_CLIENT_READY port=%d pid=%d\n", port, os.Getpid())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	parentGone := make(chan struct{})
	if watchStdin {
		go func() {
			reader := bufio.NewReader(os.Stdin)
			for {
				line, readErr := reader.ReadString('\n')
				if strings.TrimSpace(line) == "quit" || readErr != nil {
					close(parentGone)
					return
				}
			}
		}()
	}
	select {
	case err = <-done:
	case <-stop:
	case <-parentGone:
		fmt.Println("父进程已退出，后端正在关闭")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	a.CloseDesktop()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
