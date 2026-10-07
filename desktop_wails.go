//go:build (darwin || windows) && cgo && !bindings

package main

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"strings"

	"juku/internal/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:internal/webui
var desktopAssets embed.FS

func init() {
	app.LaunchDesktop = launchDesktop
}

func launchDesktop(ui *app.UIApp) error {
	return wails.Run(&options.App{
		Title:            "果果剧库",
		Width:            1280,
		Height:           840,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: options.NewRGBA(9, 9, 11, 255),
		AssetServer: &assetserver.Options{
			Assets:     desktopAssets,
			Handler:    ui.WindowHandler(),
			Middleware: reactDocumentMiddleware,
		},
		OnDomReady: func(context.Context) {
			fmt.Println("桌面窗口已显示果果剧库")
		},
	})
}

func reactDocumentMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && (request.URL.Path == "/" || request.URL.Path == "/index.html") {
			recorder := &reactDocumentRecorder{ResponseWriter: writer}
			next.ServeHTTP(recorder, request)
			body := recorder.snippet.String()
			if strings.Contains(body, "/assets/preferences.js") && strings.Contains(body, "果果剧库") && !strings.Contains(body, `id="root"`) {
				fmt.Println("桌面文档: 原页面")
			}
			return
		}
		next.ServeHTTP(writer, request)
	})
}

type reactDocumentRecorder struct {
	http.ResponseWriter
	snippet strings.Builder
}

func (recorder *reactDocumentRecorder) Write(body []byte) (int, error) {
	if recorder.snippet.Len() < 4096 {
		remain := 4096 - recorder.snippet.Len()
		if len(body) < remain {
			recorder.snippet.Write(body)
		} else {
			recorder.snippet.Write(body[:remain])
		}
	}
	return recorder.ResponseWriter.Write(body)
}
