package app

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type mergeMediaInfo struct {
	width, height              int
	audio                      bool
	duration                   time.Duration
	videoCodec, audioCodec     string
	videoExtra, audioExtra     string
	videoTB, audioTB           string
	videoProfile, audioProfile string
	pixelFormat, sar, layout   string
	sampleRate                 int
	frameRate                  float64
	reorder                    bool
}

var mergeVideoDescription = regexp.MustCompile(`Video: [a-zA-Z0-9_]+(?: \(([^)]*)\))?`)
var mergeAudioDescription = regexp.MustCompile(`Audio: [a-zA-Z0-9_]+(?: \(([^)]*)\))?`)
var mergePixelFormat = regexp.MustCompile(`, ([a-zA-Z0-9_]+)(?:\([^)]*\))?, \d+x\d+`)
var mergeFrameRate = regexp.MustCompile(`, ([0-9.]+) (?:fps|tbr)`)
var mergeStreamIndex = regexp.MustCompile(`Stream #0:(\d+)`)

func probeMergeMedia(ctx context.Context, ffmpeg, path string) (mergeMediaInfo, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "info",
		"-protocol_whitelist", "file,pipe", "-i", path, "-map", "0:v:0", "-map", "0:a:0?",
		"-c", "copy", "-t", "0", "-f", "framehash", "-hash", "sha256", "pipe:1")
	output := &cappedStringWriter{limit: 64 * 1024}
	diagnostics := &cappedStringWriter{limit: 64 * 1024}
	command.Stdout, command.Stderr = output, diagnostics
	if err := command.Run(); err != nil {
		if probeCtx.Err() != nil {
			return mergeMediaInfo{}, probeCtx.Err()
		}
		return mergeMediaInfo{}, fmt.Errorf("检测 %s 失败：%w %s", filepath.Base(path), err, truncate(diagnostics.String(), 1000))
	}
	var info mergeMediaInfo
	fields := make(map[string]string)
	for _, line := range strings.Split(output.String(), "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
			fields[key] = strings.TrimSpace(value)
		}
		if strings.HasPrefix(line, "#extradata ") {
			parts := strings.Split(line, ",")
			if len(parts) == 3 {
				if strings.TrimSpace(parts[0]) == "#extradata 0" {
					info.videoExtra = strings.TrimSpace(parts[2])
				} else if strings.TrimSpace(parts[0]) == "#extradata 1" {
					info.audioExtra = strings.TrimSpace(parts[2])
				}
			}
		}
	}
	_, _ = fmt.Sscanf(fields["#dimensions 0"], "%dx%d", &info.width, &info.height)
	info.audio = fields["#media_type 1"] == "audio"
	info.videoCodec, info.audioCodec = fields["#codec_id 0"], fields["#codec_id 1"]
	info.videoTB, info.audioTB = fields["#tb 0"], fields["#tb 1"]
	info.sar = fields["#sar 0"]
	if info.sar == "" || info.sar == "0/1" {
		info.sar = "1/1"
	}
	info.sampleRate, _ = strconv.Atoi(fields["#sample_rate 1"])
	info.layout = firstNonEmpty(fields["#channel_layout_name 1"], fields["#channel_layout 1"])
	body, _, _ := strings.Cut(diagnostics.String(), "Stream mapping:")
	if match := playbackDurationPattern.FindStringSubmatch(body); len(match) == 4 {
		hours, _ := strconv.ParseFloat(match[1], 64)
		minutes, _ := strconv.ParseFloat(match[2], 64)
		seconds, _ := strconv.ParseFloat(match[3], 64)
		info.duration = time.Duration((hours*3600 + minutes*60 + seconds) * float64(time.Second))
	}
	streams, video, audio := 0, false, false
	for _, line := range strings.Split(body, "\n") {
		index := mergeStreamIndex.FindStringSubmatch(line)
		if len(index) != 2 {
			continue
		}
		streams++
		if strings.Contains(line, "Video:") && !video {
			video = true
			info.reorder = info.reorder || index[1] != "0"
			if match := mergeVideoDescription.FindStringSubmatch(line); len(match) == 2 {
				info.videoProfile = match[1]
			}
			if match := mergePixelFormat.FindStringSubmatch(line); len(match) == 2 {
				info.pixelFormat = match[1]
			}
			if match := mergeFrameRate.FindStringSubmatch(line); len(match) == 2 {
				info.frameRate, _ = strconv.ParseFloat(match[1], 64)
			}
		} else if strings.Contains(line, "Audio:") && !audio {
			audio = true
			info.reorder = info.reorder || index[1] != "1"
			if match := mergeAudioDescription.FindStringSubmatch(line); len(match) == 2 {
				info.audioProfile = match[1]
			}
		}
	}
	info.reorder = info.reorder || streams > 2
	if info.width <= 0 || info.height <= 0 || info.duration <= 0 || info.videoCodec == "" {
		return mergeMediaInfo{}, fmt.Errorf("%s 缺少有效的视频编码、尺寸或时长，无法安全合并", filepath.Base(path))
	}
	return info, nil
}
