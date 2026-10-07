//go:build (darwin || windows) && cgo && bindings

package main

import (
	"juku/internal/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

func init() {
	app.LaunchDesktop = generateDesktopBindings
}

func generateDesktopBindings(*app.UIApp) error {
	return wails.Run(&options.App{Title: "果果剧库"})
}
