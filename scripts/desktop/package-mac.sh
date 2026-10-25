#!/usr/bin/env bash
# macOS：universal（arm64 + x86_64）Byte TV.app 与 .dmg。必须在 macOS 上运行（需要 Xcode Command Line Tools）。
#
# 可选环境变量：
#   VERSION                       版本号（默认 0.1.4）
#   MAC_SIGN_IDENTITY             "Developer ID Application: Name (TEAMID)"；为空则 ad-hoc 签名
#   NOTARY_PROFILE                xcrun notarytool store-credentials 保存的 profile 名；或
#   APPLE_ID / APPLE_TEAM_ID / APPLE_APP_PASSWORD   直接提供公证凭据
#   BYTETV_UPDATE_URL / BYTETV_UPDATE_PUBKEY        自动更新源与 ed25519 公钥（base64）
source "$(dirname "$0")/common.sh"
[ "$(uname)" = "Darwin" ] || { echo "package-mac.sh 只能在 macOS 上运行"; exit 1; }
build_frontend
cd "$ROOT"
export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=11.0
for arch in arm64 amd64; do
  echo "==> go build darwin/$arch"
  GOOS=darwin GOARCH=$arch CGO_CFLAGS="-mmacosx-version-min=11.0" CGO_LDFLAGS="-mmacosx-version-min=11.0" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/bytetv-darwin-$arch" ./cmd/bytetv
done
lipo -create -output "$OUT/bytetv-darwin-universal" "$OUT/bytetv-darwin-arm64" "$OUT/bytetv-darwin-amd64"

APP="$OUT/Byte TV.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$OUT/bytetv-darwin-universal" "$APP/Contents/MacOS/bytetv"
sed "s/__VERSION__/$VERSION/g" packaging/darwin/Info.plist.tmpl > "$APP/Contents/Info.plist"

# 图标：程序生成 1024px PNG → iconset → icns
ICONSET="$OUT/icon.iconset"; rm -rf "$ICONSET"; mkdir -p "$ICONSET"
"$OUT/bytetv-darwin-$(uname -m | sed 's/x86_64/amd64/')" -gen-icon-mac 1024 "$OUT/icon-1024.png"
for s in 16 32 128 256 512; do
  sips -z $s $s "$OUT/icon-1024.png" --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z $((s*2)) $((s*2)) "$OUT/icon-1024.png" --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"

if [ -n "${MAC_SIGN_IDENTITY:-}" ]; then
  echo "==> codesign ($MAC_SIGN_IDENTITY)"
  codesign --force --deep --options runtime --timestamp --entitlements packaging/darwin/entitlements.plist --sign "$MAC_SIGN_IDENTITY" "$APP"
else
  echo "==> ad-hoc 签名（未设置 MAC_SIGN_IDENTITY；仅本机可用，分发需 Developer ID）"
  codesign --force --deep --sign - --entitlements packaging/darwin/entitlements.plist "$APP"
fi
codesign --verify --deep --strict "$APP"

DMG="$OUT/ByteTV-$VERSION-universal.dmg"
rm -f "$DMG"
STAGE="$OUT/dmg"; rm -rf "$STAGE"; mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"; ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "Byte TV" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
rm -rf "$STAGE"
[ -n "${MAC_SIGN_IDENTITY:-}" ] && codesign --force --timestamp --sign "$MAC_SIGN_IDENTITY" "$DMG"

if [ -n "${MAC_SIGN_IDENTITY:-}" ] && { [ -n "${NOTARY_PROFILE:-}" ] || [ -n "${APPLE_ID:-}" ]; }; then
  echo "==> notarize"
  if [ -n "${NOTARY_PROFILE:-}" ]; then
    xcrun notarytool submit "$DMG" --keychain-profile "$NOTARY_PROFILE" --wait
  else
    xcrun notarytool submit "$DMG" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_PASSWORD" --wait
  fi
  xcrun stapler staple "$APP"
  xcrun stapler staple "$DMG"
fi
du -sh "$APP" "$DMG"
