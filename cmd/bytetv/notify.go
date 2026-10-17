package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// newNotificationService 创建系统通知服务。
//
// macOS 上 Wails v3 的 notifications 服务在 Startup 阶段会检查进程是否运行在
// 带 CFBundleIdentifier 的 .app 包里，裸二进制（go run ./cmd/bytetv 或直接跑
// bin/bytetv）会直接返回错误，导致 application.Run() 失败、窗口起不来。
// 这里先判断运行环境：不在 .app 包里就跳过注册，通知功能静默降级，
// 其余功能（窗口、下载、播放）不受影响。
func newNotificationService() (*notifications.NotificationService, []application.Service) {
	if !notificationsAvailable() {
		return nil, nil
	}
	service := notifications.New()
	return service, []application.Service{application.NewService(service)}
}

func notificationsAvailable() bool {
	if runtime.GOOS != "darwin" {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// 打包后的路径形如 /Applications/Byte TV.app/Contents/MacOS/bytetv
	return strings.Contains(exe, ".app/Contents/MacOS/")
}
