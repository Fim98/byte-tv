package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"

	"juku/internal/app"
)

type shell struct {
	dataDir, logDir string
	token           string
	port            int
	url             string
	ready           chan struct{}
	readyOnce       sync.Once

	app    *application.App
	win    *application.WebviewWindow
	tray   *application.SystemTray
	notify *notifications.NotificationService

	forceQuit    atomic.Bool
	quitAsking   atomic.Bool
	power        powerAssertion
	lastActive   int
	updaterReady bool
}

func newShell(dataDir, logDir string) *shell {
	return &shell{dataDir: dataDir, logDir: logDir, ready: make(chan struct{})}
}

// ---- app.ClientHost ----

func (s *shell) ClientReady(port int, url string) {
	s.port, s.url = port, url
	s.readyOnce.Do(func() { close(s.ready) })
}

func (s *shell) Native(action string, body json.RawMessage) (any, error) {
	var args map[string]any
	_ = json.Unmarshal(body, &args)
	str := func(key string) string { v, _ := args[key].(string); return v }
	switch action {
	case "pickDirectory":
		dialog := s.app.Dialog.OpenFile().CanChooseDirectories(true).CanChooseFiles(false).CanCreateDirectories(true).SetTitle("选择下载目录")
		if initial := str("initial"); initial != "" {
			dialog.SetDirectory(initial)
		}
		if s.win != nil {
			dialog.AttachToWindow(s.win)
		}
		path, err := dialog.PromptForSingleSelection()
		if err != nil {
			return "", nil // 用户取消
		}
		return path, nil
	case "showItemInFolder":
		path := str("path")
		if path == "" {
			return false, fmt.Errorf("缺少路径")
		}
		info, err := os.Stat(path)
		if err != nil {
			return false, err
		}
		return true, s.app.Env.OpenFileManager(path, !info.IsDir())
	case "openExternal":
		u := str("url")
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return false, fmt.Errorf("只允许 http(s) 链接")
		}
		return true, s.app.Browser.OpenURL(u)
	case "toggleFullScreen":
		// macOS WKWebView 的 HTML5 全屏在部分版本不可用，退回原生窗口全屏。
		if s.win != nil {
			application.InvokeSync(func() { s.win.ToggleFullscreen() })
			return s.win.IsFullscreen(), nil
		}
		return false, nil
	case "startDrag":
		// 拖动窗口。页面走本机 HTTP、不经过 Wails AssetServer，拿不到 Wails 运行时，
		// 因此 --wails-draggable / -webkit-app-region 都不会生效；改由前端在按下时
		// 调用这里，转发给 Wails 的平台实现（macOS performWindowDragWithEvent /
		// Windows WM_NCLBUTTONDOWN+HTCAPTION）。需在按住时调用，macOS 依赖当前鼠标事件。
		if s.win == nil {
			return false, nil
		}
		application.InvokeSync(func() { s.win.HandleMessage("wails:drag") })
		return true, nil
	case "getVersion":
		return version, nil
	case "getSettings":
		return map[string]any{"closeToTray": s.closeToTray(), "autoUpdate": updateURL != ""}, nil
	case "setSettings":
		if v, ok := args["closeToTray"].(bool); ok {
			s.writePref("bytetv.closeToTray", v)
		}
		return map[string]any{"closeToTray": s.closeToTray()}, nil
	case "checkForUpdates":
		return s.checkForUpdates(), nil
	case "notify":
		s.sendNotification(str("title"), str("body"))
		return true, nil
	case "log":
		if os.Getenv("BYTETV_AUTOTEST_JS") == "" {
			return nil, fmt.Errorf("log 仅用于自动化测试")
		}
		fmt.Println("AUTOTEST", string(body))
		return true, nil
	case "quit":
		s.app.Quit()
		return true, nil
	}
	return nil, fmt.Errorf("未知操作：%s", action)
}

// ---- 偏好 ----

func (s *shell) prefs() map[string]any {
	prefs := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(s.dataDir, "desktop_prefs.json")); err == nil {
		_ = json.Unmarshal(data, &prefs)
	}
	return prefs
}

func (s *shell) closeToTray() bool {
	v, ok := s.prefs()["bytetv.closeToTray"].(bool)
	return !ok || v
}

func (s *shell) writePref(key string, value any) {
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/desktop/prefs", s.port), strings.NewReader(fmt.Sprintf(`{%q:%s}`, key, mustJSON(value))))
	req.Header.Set("X-Juku-Token", s.token)
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// ---- 主流程 ----

func (s *shell) run() {
	s.notify = notifications.New()
	s.app = application.New(application.Options{
		Name:        "Byte TV",
		Description: "Byte TV",
		Icon:        appIconPNG(256),
		Services:    []application.Service{application.NewService(s.notify)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		Linux:       application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ProgramName: "bytetv"},
		Windows:     application.WindowsOptions{},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               appID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { s.showWindow() },
		},
		ShouldQuit: s.shouldQuit,
		OnShutdown: func() {
			s.power.release()
			app.StopClient()
		},
		PanicHandler: func(details *application.PanicDetails) {
			s.writeCrash("shell panic", fmt.Sprintf("%v\n%s", details.Error, details.StackTrace))
		},
	})

	s.win = s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Byte TV",
		Width:            1440,
		Height:           900,
		MinWidth:         1024,
		MinHeight:        680,
		URL:              s.url,
		JS:               autotestJS(),
		BackgroundColour: application.NewRGB(10, 10, 12),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
			Backdrop: application.MacBackdropNormal,
		},
		Windows: application.WindowsWindow{},
	})
	s.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if s.forceQuit.Load() {
			return
		}
		if s.closeToTray() {
			e.Cancel()
			s.win.Hide()
			return
		}
		e.Cancel()
		s.app.Quit()
	})

	s.app.Menu.Set(s.buildMenu())
	s.buildTray()
	s.initUpdater()

	go s.monitorActivity()
	go s.watchdog()
	s.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			if runtime.GOOS == "darwin" {
				_, _ = s.notify.RequestNotificationAuthorization()
			}
		}()
	})

	if err := s.app.Run(); err != nil {
		s.writeCrash("app.Run", err.Error())
		log.Fatal(err)
	}
}

func (s *shell) showWindow() {
	if s.win == nil {
		return
	}
	application.InvokeAsync(func() {
		s.win.Show()
		s.win.UnMinimise()
		s.win.Focus()
	})
}

func (s *shell) navigate(path string) {
	s.showWindow()
	app.ClientEmit("navigate", path)
}

func (s *shell) buildMenu() *application.Menu {
	menu := s.app.NewMenu()
	if runtime.GOOS == "darwin" {
		appMenu := menu.AddSubmenu("Byte TV")
		appMenu.Add("关于 Byte TV").OnClick(func(*application.Context) {
			s.app.Dialog.Info().SetTitle("Byte TV").SetMessage("版本 " + version).SetIcon(appIconPNG(128)).Show()
		})
		appMenu.AddSeparator()
		appMenu.Add("设置…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) { s.navigate("/settings") })
		appMenu.Add("检查更新…").OnClick(func(*application.Context) { go s.checkForUpdatesInteractive() })
		appMenu.AddSeparator()
		appMenu.AddRole(application.Hide)
		appMenu.AddRole(application.HideOthers)
		appMenu.AddRole(application.UnHide)
		appMenu.AddSeparator()
		appMenu.Add("退出 Byte TV").SetAccelerator("CmdOrCtrl+q").OnClick(func(*application.Context) { s.app.Quit() })
	} else {
		file := menu.AddSubmenu("文件")
		file.Add("设置").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) { s.navigate("/settings") })
		file.Add("检查更新…").OnClick(func(*application.Context) { go s.checkForUpdatesInteractive() })
		file.AddSeparator()
		file.Add("退出").SetAccelerator("CmdOrCtrl+q").OnClick(func(*application.Context) { s.app.Quit() })
	}
	menu.AddRole(application.EditMenu)
	view := menu.AddSubmenu("显示")
	view.Add("刷新").SetAccelerator("CmdOrCtrl+r").OnClick(func(*application.Context) { s.win.Reload() })
	view.Add("切换全屏").SetAccelerator("Ctrl+CmdOrCtrl+f").OnClick(func(*application.Context) { s.win.ToggleFullscreen() })
	if os.Getenv("BYTETV_DEVTOOLS") == "1" {
		view.Add("开发者工具").SetAccelerator("Alt+CmdOrCtrl+i").OnClick(func(*application.Context) { s.win.OpenDevTools() })
	}
	goMenu := menu.AddSubmenu("前往")
	for i, item := range []struct{ label, path string }{{"首页", "/home"}, {"浏览", "/browse"}, {"追剧", "/following"}, {"下载", "/downloads"}} {
		item := item
		goMenu.Add(item.label).SetAccelerator(fmt.Sprintf("CmdOrCtrl+%d", i+1)).OnClick(func(*application.Context) { s.navigate(item.path) })
	}
	menu.AddRole(application.WindowMenu)
	return menu
}

func (s *shell) buildTray() {
	s.tray = s.app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		s.tray.SetTemplateIcon(trayTemplatePNG())
	} else {
		s.tray.SetIcon(appIconPNG(64))
	}
	s.tray.SetTooltip("Byte TV")
	menu := s.app.NewMenu()
	menu.Add("显示 Byte TV").OnClick(func(*application.Context) { s.showWindow() })
	menu.Add("下载任务").OnClick(func(*application.Context) { s.navigate("/downloads") })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { s.app.Quit() })
	s.tray.SetMenu(menu)
	s.tray.OnClick(func() { s.showWindow() })
}

// shouldQuit：有下载/合并任务时先确认（对话框是异步的，确认后再次 Quit）。
func (s *shell) shouldQuit() bool {
	if s.forceQuit.Load() {
		return true
	}
	downloads, merges := app.ClientActivity()
	if downloads+merges == 0 {
		s.forceQuit.Store(true)
		return true
	}
	if s.quitAsking.Swap(true) {
		return false
	}
	s.showWindow()
	dialog := s.app.Dialog.Question().SetTitle("确定退出 Byte TV？").
		SetMessage(fmt.Sprintf("还有 %d 个下载任务、%d 个合并任务正在进行，退出会中断它们（下次启动可继续）。", downloads, merges))
	quit := dialog.AddButton("退出")
	quit.OnClick(func() {
		s.forceQuit.Store(true)
		s.app.Quit()
	})
	cancel := dialog.AddButton("取消")
	cancel.OnClick(func() { s.quitAsking.Store(false) })
	dialog.SetDefaultButton(cancel).SetCancelButton(cancel)
	if s.win != nil {
		dialog.AttachToWindow(s.win)
	}
	dialog.Show()
	return false
}

// monitorActivity：托盘提示、阻止休眠、完成通知。
func (s *shell) monitorActivity() {
	defer recoverInto(s, "activity monitor")
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		downloads, merges := app.ClientActivity()
		active := downloads + merges
		if active > 0 {
			s.power.acquire("Byte TV 正在下载/合并视频")
			s.tray.SetTooltip(fmt.Sprintf("Byte TV · %d 个下载、%d 个合并进行中", downloads, merges))
		} else {
			s.power.release()
			s.tray.SetTooltip("Byte TV")
			if s.lastActive > 0 {
				s.sendNotification("Byte TV", "下载与合并任务已全部完成")
			}
		}
		s.lastActive = active
	}
}

// watchdog：后端健康检查连续失败时提示重启（同进程，无法单独拉起后端）。
func (s *shell) watchdog() {
	defer recoverInto(s, "watchdog")
	client := &http.Client{Timeout: 5 * time.Second}
	failures := 0
	for range time.Tick(10 * time.Second) {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/desktop/health", s.port), nil)
		req.Header.Set("X-Juku-Token", s.token)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		if err != nil || resp.StatusCode != http.StatusOK {
			failures++
		} else {
			failures = 0
		}
		if failures == 3 {
			s.writeCrash("watchdog", fmt.Sprintf("health check failed: %v", err))
			s.offerRestart("Byte TV 后台服务无响应", "后台服务连续 30 秒没有响应。重新启动应用可恢复（下载任务会在启动后继续）。")
		}
	}
}

func (s *shell) offerRestart(title, message string) {
	if s.app == nil {
		return
	}
	dialog := s.app.Dialog.Error().SetTitle(title).SetMessage(message)
	restart := dialog.AddButton("重新启动")
	restart.OnClick(func() { s.relaunch() })
	later := dialog.AddButton("稍后")
	dialog.SetDefaultButton(restart).SetCancelButton(later)
	dialog.Show()
}

func (s *shell) relaunch() {
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command(exe)
		cmd.Env = os.Environ()
		_ = cmd.Start()
	}
	s.forceQuit.Store(true)
	s.app.Quit()
}

func (s *shell) backendCrashed(detail string) {
	s.writeCrash("backend panic", detail)
	s.offerRestart("Byte TV 后台服务异常退出", "后台服务发生错误已停止，详情已写入日志目录。")
}

func (s *shell) backendStopped() {
	if !s.forceQuit.Load() && s.app != nil {
		s.offerRestart("Byte TV 后台服务已停止", "后台服务意外停止。")
	}
}

func (s *shell) writeCrash(kind, detail string) {
	name := filepath.Join(s.logDir, "crash-"+time.Now().Format("20060102-150405")+".log")
	_ = os.WriteFile(name, []byte(kind+"\n"+detail+"\n"), 0o600)
	log.Printf("%s: %s", kind, detail)
}

func recoverInto(s *shell, name string) {
	if value := recover(); value != nil {
		s.writeCrash(name+" panic", fmt.Sprint(value))
	}
}

func (s *shell) sendNotification(title, body string) {
	if title == "" {
		title = "Byte TV"
	}
	err := s.notify.SendNotification(notifications.NotificationOptions{ID: fmt.Sprintf("bytetv-%d", time.Now().UnixNano()), Title: title, Body: body})
	if err != nil {
		log.Printf("通知发送失败：%v", err)
	}
}

// ---- 自动更新：编译时注入 updateURL（endpoint manifest，支持 {{platform}}/{{arch}} 占位）启用 ----

func (s *shell) initUpdater() {
	if updateURL == "" {
		return
	}
	provider, err := endpoint.New(endpoint.Config{URL: updateURL})
	if err != nil {
		log.Printf("更新源配置无效：%v", err)
		return
	}
	cfg := updater.Config{CurrentVersion: strings.TrimPrefix(version, "v"), Providers: []updater.Provider{provider}, CheckInterval: 6 * time.Hour}
	if key := updatePublicKey(); len(key) > 0 {
		cfg.PublicKey = key
	}
	if err := s.app.Updater.Init(cfg); err != nil {
		log.Printf("自动更新初始化失败：%v", err)
		return
	}
	s.updaterReady = true
}

func (s *shell) checkForUpdates() map[string]any {
	if !s.updaterReady {
		return map[string]any{"ok": false, "message": "此版本未配置自动更新"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := s.app.Updater.Check(ctx)
	if err != nil {
		return map[string]any{"ok": false, "message": err.Error()}
	}
	if release == nil {
		return map[string]any{"ok": true, "available": false, "message": "已是最新版本"}
	}
	go func() {
		if err := s.app.Updater.DownloadAndInstall(context.Background()); err != nil {
			log.Printf("更新安装失败：%v", err)
		}
	}()
	return map[string]any{"ok": true, "available": true, "version": release.Version, "message": "发现新版本 " + release.Version + "，正在下载"}
}

func (s *shell) checkForUpdatesInteractive() {
	result := s.checkForUpdates()
	message, _ := result["message"].(string)
	s.app.Dialog.Info().SetTitle("检查更新").SetMessage(message).Show()
}
