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
BOOT="$WIN/MicrosoftEdgeWebview2Setup.exe"
# 内置 WebView2 Evergreen Bootstrapper：缺失时下载（带重试，避免 CI 网络抖动中断打包）
if [ ! -s "$BOOT" ]; then
  ok=0
  for i in 1 2 3; do
    echo "==> 下载 WebView2 引导程序（第 $i 次尝试）"
    if curl -fsSL --retry 3 --retry-delay 2 -o "$BOOT" "https://go.microsoft.com/fwlink/p/?LinkId=2124703" && [ -s "$BOOT" ]; then
      ok=1
      break
    fi
    sleep 3
  done
  [ "$ok" = "1" ] || { echo "WebView2 引导程序下载失败，请检查网络或预先放入 $BOOT"; exit 1; }
fi
echo "WebView2 引导程序：$(du -h "$BOOT" | cut -f1)"
# makensis 相对 .nsi 所在目录解析 File 路径；这里统一转绝对路径，避免工作目录差异
ABS_BOOT="$(cd "$(dirname "$BOOT")" && pwd)/$(basename "$BOOT")"
ABS_WIN="$(cd "$WIN" && pwd)"
for arch in amd64 arm64; do
  makensis -V2 -DVERSION="$VERSION" -DARCH="$arch" -DBIN="$ABS_WIN/bytetv-$arch.exe" -DBOOTSTRAPPER="$ABS_BOOT" \
    -DICON="$ROOT/packaging/windows/icon.ico" -DOUTFILE="$WIN/ByteTV-$VERSION-$arch-setup.exe" packaging/windows/installer.nsi
done
ls -lh "$WIN"/*.exe
