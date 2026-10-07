#!/usr/bin/env bash
# 开发运行：前端用磁盘上的 frontend/dist（改完前端 npm run build 后刷新窗口即可），不必重新嵌入。
source "$(dirname "$0")/common.sh"
cd "$ROOT"
[ -f frontend/dist/index.html ] || (cd frontend && npm run build)
TAGS=""; [ "$(go env GOOS)" = "linux" ] && TAGS="-tags gtk3"
BYTETV_UI_DIR="$ROOT/frontend/dist" BYTETV_DEVTOOLS=1 go run $TAGS ./cmd/bytetv
