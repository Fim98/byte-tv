#!/bin/bash
# Linux xvfb 冒烟测试：启动 bin/bytetv，注入 play.js，用 xdotool 发空格（真实用户手势）开始播放，截图。
# 需要 Xvfb、xdotool、ffmpeg、pulseaudio、GStreamer；play.js 里的剧 ID 按本地剧库调整。
export HOME=${SMOKE_HOME:-/tmp/jukuhome} BYTETV_DATA=${BYTETV_DATA:-/tmp/jukudata} BYTETV_AUTOTEST_JS="$(dirname "$0")/play.js"
Xvfb :77 -screen 0 1600x1000x24 >/dev/null 2>&1 &
XV=$!; export DISPLAY=:77; sleep 1
pulseaudio -D --exit-idle-time=-1 --disallow-exit 2>/dev/null; sleep 1
dbus-run-session -- "$(dirname "$0")/../../../bin/bytetv" > /tmp/bytetv.log 2>&1 &
APP=$!
for i in $(seq 1 120); do grep -q '"step":"gesture-wait"\|"step":"error"' /tmp/bytetv.log && break; sleep 1; done
WID=$(xdotool search --name "Byte TV" | head -1); xdotool windowfocus $WID 2>/dev/null; sleep 0.5; xdotool key --window $WID space
for i in $(seq 1 120); do grep -q '"step":"shot-ready"\|"step":"error"' /tmp/bytetv.log && break; sleep 1; done
ffmpeg -y -loglevel error -f x11grab -video_size 1600x1000 -i :77 -frames:v 1 ${SHOTS:-/tmp}/02-player.png
echo "elapsed $i"
