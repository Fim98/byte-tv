package main

import (
	"fmt"
	"os"
)

// autotestJS 开发/CI 冒烟测试：BYTETV_AUTOTEST_JS 指向脚本文件，作为窗口的注入脚本在页面加载完成后执行
// （页面不是 Wails 资源服务器提供的，WebviewWindow.ExecJS 会一直等待运行时而不执行，所以用 Options.JS）。
// 脚本可 POST /api/desktop/native/log 把结果打到 stdout。未设置环境变量时完全不生效。
func autotestJS() string {
	path := os.Getenv("BYTETV_AUTOTEST_JS")
	if path == "" {
		return ""
	}
	script, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("AUTOTEST read failed:", err)
		return ""
	}
	return "setTimeout(function(){" + string(script) + "}, 3000);"
}
