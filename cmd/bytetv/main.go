// Byte TV 桌面客户端（Wails v3 外壳）。
//
// 架构：同一进程内运行现有 Go 后端（客户端模式，只监听 127.0.0.1，随机/复用端口 + 启动令牌），
// WebView 直接加载本机 HTTP 地址；视频与 API 都不经过 Wails 自定义协议。
// 页面无法使用 Wails 绑定，原生能力通过后端 /api/desktop/native/* 转给本外壳（见 host.go）。
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"juku/internal/app"
)

//go:embed all:ui
var uiFS embed.FS

// 通过 -ldflags "-X main.version=1.2.3 -X main.updateURL=https://..." 注入。
var (
	version   = "0.0.0-dev"
	updateURL = ""
)

const appID = "com.bytetv.desktop"

func dataDirectory() string {
	if dir := os.Getenv("BYTETV_DATA"); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	// 与 Electron 版 userData 相同（macOS: ~/Library/Application Support/ByteTV，Windows: %APPDATA%\ByteTV）。
	return filepath.Join(base, "ByteTV")
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func main() {
	if len(os.Args) > 2 && os.Args[1] == "-gen-icon" {
		if err := os.WriteFile(os.Args[2], appIconPNG(1024), 0o644); err != nil {
			log.Fatal(err)
		}
		return
	}
	dataDir := dataDirectory()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	crashLog := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(crashLog, 0o755)

	backendArgs := []string{os.Args[0], "-client", "-client-watch-stdin=false", "-data-dir", dataDir}
	if sub, err := fs.Sub(uiFS, "ui"); err == nil {
		if _, statErr := fs.Stat(sub, "index.html"); statErr == nil {
			app.ClientUIFS = sub
		} else if dev := os.Getenv("BYTETV_UI_DIR"); dev != "" {
			backendArgs = append(backendArgs, "-client-ui", dev)
		} else {
			log.Println("警告：二进制未嵌入前端（先运行 task build:frontend 或设置 BYTETV_UI_DIR）")
		}
	}

	shell := newShell(dataDir, crashLog)
	app.SetClientHost(shell)

	token := randomToken()
	_ = os.Setenv("JUKU_CLIENT_TOKEN", token)
	shell.token = token
	os.Args = backendArgs
	go func() {
		defer func() {
			if value := recover(); value != nil {
				shell.backendCrashed(fmt.Sprintf("%v\n%s", value, debug.Stack()))
				return
			}
			shell.backendStopped()
		}()
		app.Run()
	}()

	select {
	case <-shell.ready:
	case <-time.After(60 * time.Second):
		log.Fatal("后端 60 秒内未就绪")
	}
	shell.run()
}
