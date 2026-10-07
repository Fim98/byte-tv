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
[ -f "$BOOT" ] || curl -fsSL -o "$BOOT" "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
for arch in amd64 arm64; do
  makensis -V2 -DVERSION="$VERSION" -DARCH="$arch" -DBIN="$WIN/bytetv-$arch.exe" -DBOOTSTRAPPER="$BOOT" \
    -DICON="$ROOT/packaging/windows/icon.ico" -DOUTFILE="$WIN/ByteTV-$VERSION-$arch-setup.exe" packaging/windows/installer.nsi
done
ls -lh "$WIN"/*.exe
