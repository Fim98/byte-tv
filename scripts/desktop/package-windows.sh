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

# makensis 是原生程序：在 Git Bash（MSYS）下必须传 Windows 路径，
# 否则 /d/a/... 这类 MSYS 路径它解析不到（报 "no files found"）。
# cygpath -m 产出 D:/a/... 混合格式：NSIS 能正确解析，且不含需转义的反斜杠。
# 非 MSYS 环境（macOS/Linux 交叉编译）没有 cygpath，直接用原路径。
winpath() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi
}
# makensis 的 File 指令以 .nsi 所在目录为基准解析相对路径，
# 因此统一传绝对路径最稳妥。
MK_BOOT="$(winpath "$(cd "$(dirname "$BOOT")" && pwd)/$(basename "$BOOT")")"
MK_WIN="$(winpath "$(cd "$WIN" && pwd)")"
MK_ICON="$(winpath "$(cd "$(dirname "$ROOT/packaging/windows/icon.ico")" && pwd)/icon.ico")"

for arch in amd64 arm64; do
  makensis -V2 -DVERSION="$VERSION" -DARCH="$arch" -DBIN="$MK_WIN/bytetv-$arch.exe" -DBOOTSTRAPPER="$MK_BOOT" \
    -DICON="$MK_ICON" -DOUTFILE="$MK_WIN/ByteTV-$VERSION-$arch-setup.exe" packaging/windows/installer.nsi
done
ls -lh "$WIN"/*.exe
