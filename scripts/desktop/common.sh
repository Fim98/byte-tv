#!/usr/bin/env bash
# Byte TV（Wails v3）构建公共变量。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.2}"
OUT="${OUT:-$ROOT/bin}"
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.0+auto}"
LDFLAGS="-s -w -X main.version=${VERSION}"
[ -n "${BYTETV_UPDATE_URL:-}" ] && LDFLAGS="$LDFLAGS -X main.updateURL=${BYTETV_UPDATE_URL}"
[ -n "${BYTETV_UPDATE_PUBKEY:-}" ] && LDFLAGS="$LDFLAGS -X main.updatePublicKeyB64=${BYTETV_UPDATE_PUBKEY}"
mkdir -p "$OUT"

# 构建前端并嵌入到 cmd/bytetv/ui
build_frontend() {
  if [ "${SKIP_FRONTEND:-0}" != "1" ]; then
    (cd "$ROOT/frontend" && { [ -d node_modules ] || npm ci; } && npm run build)
  fi
  find "$ROOT/cmd/bytetv/ui" -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
  cp -R "$ROOT/frontend/dist/." "$ROOT/cmd/bytetv/ui/"
}
