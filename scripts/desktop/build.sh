#!/usr/bin/env bash
# 为当前平台构建 Byte TV 可执行文件：bin/bytetv（Windows 为 bin/bytetv.exe）。
source "$(dirname "$0")/common.sh"
build_frontend
cd "$ROOT"
TAGS=""
[ "$(go env GOOS)" = "linux" ] && TAGS="-tags gtk3"   # Linux 仅用于开发/CI（WebKitGTK 4.1）
EXT=""; [ "$(go env GOOS)" = "windows" ] && EXT=".exe"
go build $TAGS -trimpath -ldflags "$LDFLAGS" -o "$OUT/bytetv$EXT" ./cmd/bytetv
ls -lh "$OUT/bytetv$EXT"
