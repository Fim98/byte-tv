package webui

import "embed"

//go:embed index.html
var HTML string

//go:embed *.js *.css *.txt
var Assets embed.FS
