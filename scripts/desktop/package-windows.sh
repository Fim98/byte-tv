#!/usr/bin/env bash
# Windows：bytetv.exe + NSIS 安装包（内置 WebView2 Evergreen Bootstrapper，缺失时静默安装）。
# 可在 Windows（Git Bash）或 macOS/Linux 上交叉编译（Wails v3 Windows 版不需要 cgo），需安装 makensis。
source "$(dirname "$0")/common.sh"
build_frontend
cd "$ROOT"
WIN="$OUT/windows"; mkdir -p "$WIN"
HOSTTAGS=""; [ "$(go env GOHOSTOS)" = "linux" ] && HOSTTAGS="-tags gtk3"
GOOS= GOARCH= go run $HOSTTAGS ./cmd/bytetv -gen-icon 256 "$WIN/icon.ico"
cp "$WIN/icon.ico" packaging/windows/icon.ico
# 可执行文件图标与清单（DPI 感知 / 公共控件 v6）
( cd cmd/bytetv && go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64,arm64 --icon ../../packaging/windows/icon.ico --manifest gui \
    --product-name "Byte TV" --file-description "Byte TV" --product-version "$VERSION" --file-version "$VERSION" )
for arch in amd64 arm64; do
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS -H windowsgui" -o "$WIN/bytetv-$arch.exe" ./cmd/bytetv
done
rm -f cmd/bytetv/rsrc_windows_*.syso
BOOT_NAME="MicrosoftEdgeWebview2Setup.exe"
BOOT="$WIN/$BOOT_NAME"
# 内置 WebView2 Evergreen Bootstrapper：缺失时下载（带重试，避免 CI 网络抖动中断打包）。
#
# 注意：Git Bash（MSYS）下不能直接用 `curl -o <MSYS路径>` —— MSYS 的 curl 会按
# MSYS 路径语义落盘，导致 makensis 这种原生程序从 Windows 视角看不到该文件
# （表现为 du 有大小、makensis 报 no files found）。
# 因此这里统一用 `cygpath -w` 把目标转成原生 Windows 路径再交给 curl。
native_path() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi
}
if [ ! -s "$BOOT" ]; then
  ok=0
  for i in 1 2 3; do
    echo "==> 下载 WebView2 引导程序（第 $i 次尝试）"
    if curl -fsSL --retry 3 --retry-delay 2 -o "$(native_path "$BOOT")" \
      "https://go.microsoft.com/fwlink/p/?LinkId=2124703" && [ -s "$BOOT" ]; then
      ok=1
      break
    fi
    sleep 3
  done
  [ "$ok" = "1" ] || { echo "WebView2 引导程序下载失败，请检查网络或预先放入 $BOOT"; exit 1; }
fi
echo "WebView2 引导程序：$(du -h "$BOOT" | cut -f1)"

# 诊断：从原生 Windows 视角确认关键文件可见（Git Bash 与原生路径视图可能不一致）
if command -v cygpath >/dev/null 2>&1; then
  echo "=== 原生侧文件可见性检查 ==="
  for f in "$BOOT" "$WIN/bytetv-amd64.exe" "$ROOT/packaging/windows/icon.ico"; do
    cmd //c "if exist \"$(cygpath -w "$f")\" (echo   FOUND $(basename "$f")) else (echo   MISSING $(basename "$f"))" || true
  done
fi

# makensis 以 .nsi 所在目录为基准解析 File 路径，且需要原生可见的路径。
# 用相对路径最稳妥：cd 进 packaging/windows 后引用 ../../bin/windows/*。
for arch in amd64 arm64; do
  (
    cd packaging/windows
    makensis -V2 \
      -DVERSION="$VERSION" -DARCH="$arch" \
      -DBIN="../../bin/windows/bytetv-$arch.exe" \
      -DBOOTSTRAPPER="../../bin/windows/$BOOT_NAME" \
      -DICON="icon.ico" \
      -DOUTFILE="../../bin/windows/ByteTV-$VERSION-$arch-setup.exe" \
      installer.nsi
  )
done
ls -lh "$WIN"/*.exe
