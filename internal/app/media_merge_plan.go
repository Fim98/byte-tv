package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type mergeMediaPlan struct {
	video, audio  mergeMediaInfo
	steps         []mergeMediaStep
	timescale     int
	transport     bool
	parameters    bool
	videoCount    int
	audioCount    int
	remuxCount    int
	audioFallback bool
}

type mergeMediaStep struct {
	video, audio, remux bool
}

func (info mergeMediaInfo) videoKey() string {
	key := fmt.Sprintf("%s|%s|%s|%dx%d|%s", info.videoCodec, info.videoProfile, info.pixelFormat, info.width, info.height, info.sar)
	if info.videoCodec != "h264" && info.videoCodec != "hevc" {
		key += "|" + info.videoExtra
	}
	return key
}

func (info mergeMediaInfo) audioKey() string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", info.audioCodec, info.audioProfile, info.sampleRate, info.layout, info.audioExtra)
}

func majorityMergeMedia(infos []mergeMediaInfo, key func(mergeMediaInfo) string, audio bool) mergeMediaInfo {
	counts := make(map[string]int)
	durations := make(map[string]time.Duration)
	for _, info := range infos {
		if audio && !info.audio {
			continue
		}
		value := key(info)
		counts[value]++
		durations[value] += info.duration
	}
	var chosen mergeMediaInfo
	bestCount, bestDuration := 0, time.Duration(0)
	for _, info := range infos {
		if audio && !info.audio {
			continue
		}
		value := key(info)
		if counts[value] > bestCount || counts[value] == bestCount && durations[value] > bestDuration {
			chosen, bestCount, bestDuration = info, counts[value], durations[value]
		}
	}
	return chosen
}

func planMergeMedia(infos []mergeMediaInfo) mergeMediaPlan {
	plan := mergeMediaPlan{timescale: 90000}
	plan.video = majorityMergeMedia(infos, mergeMediaInfo.videoKey, false)
	var compatible []mergeMediaInfo
	for _, info := range infos {
		if info.videoKey() == plan.video.videoKey() {
			compatible = append(compatible, info)
		}
	}
	plan.video = majorityMergeMedia(compatible, func(info mergeMediaInfo) string { return info.videoTB }, false)
	plan.audio = majorityMergeMedia(infos, mergeMediaInfo.audioKey, true)
	if plan.audio.audio && plan.audio.audioCodec == "aac" && plan.audio.audioProfile != "" && plan.audio.audioProfile != "LC" {
		for _, info := range infos {
			if !info.audio || info.audioKey() != plan.audio.audioKey() {

				plan.audioFallback = true
				plan.audio.audioProfile, plan.audio.audioExtra = "LC", ""
				break
			}
		}
	}
	if value, ok := strings.CutPrefix(plan.video.videoTB, "1/"); ok {
		if scale, err := strconv.Atoi(value); err == nil && scale > 0 && scale <= 10000000 {
			plan.timescale = scale
		}
	}
	for _, info := range infos {
		step := mergeMediaStep{video: info.videoKey() != plan.video.videoKey(), audio: plan.audio.audio && (plan.audioFallback || !info.audio || info.audioKey() != plan.audio.audioKey())}
		if step.video || info.videoExtra != plan.video.videoExtra {
			plan.parameters = true
		}
		step.remux = info.videoTB != fmt.Sprintf("1/%d", plan.timescale) || plan.audio.audio && info.audioTB != plan.audio.audioTB || info.reorder
		plan.steps = append(plan.steps, step)
	}
	plan.transport = plan.video.videoCodec == "hevc" && plan.parameters
	for i := range plan.steps {
		step := &plan.steps[i]
		step.remux = step.remux || plan.transport
		if step.video {
			plan.videoCount++
		}
		if step.audio {
			plan.audioCount++
		}
		if step.remux && !step.video && !step.audio {
			plan.remuxCount++
		}
	}
	return plan
}

func (plan mergeMediaPlan) description() string {
	parts := []string{}
	if plan.videoCount > 0 {
		parts = append(parts, fmt.Sprintf("仅转视频 %d/%d 集（%s %dx%d）", plan.videoCount, len(plan.steps), strings.ToUpper(plan.video.videoCodec), plan.video.width, plan.video.height))
	}
	if plan.audioCount > 0 {
		if plan.audioFallback {
			parts = append(parts, fmt.Sprintf("统一音轨为 AAC-LC %d/%d 集", plan.audioCount, len(plan.steps)))
		} else {
			parts = append(parts, fmt.Sprintf("调整音轨 %d/%d 集", plan.audioCount, len(plan.steps)))
		}
	}
	if len(parts) == 0 {
		if plan.remuxCount > 0 {
			return "无损换封装合并（不转码）"
		}
		return "原编码快速合并（不转码）"
	}
	return strings.Join(parts, "；")
}

func mergeVideoEncoder(info mergeMediaInfo) ([]string, error) {
	profile := strings.ToLower(info.videoProfile)
	var args []string
	switch info.videoCodec {
	case "h264":
		profiles := map[string]string{"constrained baseline": "baseline", "baseline": "baseline", "main": "main", "high": "high", "high 10": "high10", "high 4:2:2": "high422", "high 4:4:4 predictive": "high444"}
		value, ok := profiles[profile]
		if !ok {
			return nil, fmt.Errorf("暂不能将少数分集转为多数使用的 H.264 %s", info.videoProfile)
		}
		args = []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-profile:v", value, "-threads:v", "2"}
	case "hevc":
		profiles := map[string]string{"main": "main", "main 10": "main10"}
		value, ok := profiles[profile]
		if !ok {
			return nil, fmt.Errorf("暂不能将少数分集转为多数使用的 HEVC %s", info.videoProfile)
		}
		args = []string{"-c:v", "libx265", "-preset", "veryfast", "-crf", "18", "-profile:v", value, "-threads:v", "2", "-x265-params", "pools=2:frame-threads=2:log-level=error"}
	default:
		return nil, fmt.Errorf("暂不支持将少数分集转为多数使用的 %s 编码；未对整剧转码", info.videoCodec)
	}
	if info.pixelFormat == "" || info.width%2 != 0 || info.height%2 != 0 {
		return nil, fmt.Errorf("多数分集的像素格式或尺寸无法安全转换")
	}
	return append(args, "-pix_fmt", info.pixelFormat), nil
}

func mergeAudioEncoder(info mergeMediaInfo) ([]string, error) {
	encoder := info.audioCodec
	switch encoder {
	case "aac":
		if info.audioProfile != "" && info.audioProfile != "LC" {
			return nil, fmt.Errorf("暂不能将少数音轨转为多数使用的 AAC %s", info.audioProfile)
		}
	case "mp3":
		encoder = "libmp3lame"
	case "ac3", "eac3", "alac":
	default:
		return nil, fmt.Errorf("暂不支持将少数音轨转为多数使用的 %s", info.audioCodec)
	}
	if info.sampleRate <= 0 || info.layout == "" {
		return nil, fmt.Errorf("多数音轨缺少采样率或声道布局，无法安全转换")
	}
	args := []string{"-c:a", encoder, "-ar", strconv.Itoa(info.sampleRate), "-channel_layout:a", info.layout}
	if encoder == "aac" {
		args = append(args, "-profile:a", "aac_low", "-b:a", "192k")
	}
	return args, nil
}

func (plan mergeMediaPlan) prepareArgs(input, output string, info mergeMediaInfo, step mergeMediaStep) ([]string, error) {
	args := []string{"-xerror", "-threads", "2", "-protocol_whitelist", "file,pipe", "-i", input}
	if plan.audio.audio && !info.audio {
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("anullsrc=channel_layout=%s:sample_rate=%d", plan.audio.layout, plan.audio.sampleRate))
	}
	args = append(args, "-map", "0:v:0", "-sn", "-dn", "-map_metadata", "-1", "-c:v", "copy")
	if step.video {
		encoder, err := mergeVideoEncoder(plan.video)
		if err != nil {
			return nil, err
		}
		sar := plan.video.sar
		if sar == "" || sar == "0/1" {
			sar = "1/1"
		}
		filter := "setpts=PTS-STARTPTS,"
		if plan.video.frameRate > 0 {
			filter += "fps=" + strconv.FormatFloat(plan.video.frameRate, 'f', -1, 64) + ","
		}
		filter += fmt.Sprintf("scale=w='max(2,trunc(min(%d,%d*dar/(%s))/2)*2)':h='max(2,trunc(min(%d,%d*(%s)/dar)/2)*2)',pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=%s", plan.video.width, plan.video.height, sar, plan.video.height, plan.video.width, sar, plan.video.width, plan.video.height, sar)
		args = append(args, "-vf", filter)
		args = append(args, encoder...)
	}
	if plan.audio.audio {
		stream := "0:a:0"
		if !info.audio {
			stream = "1:a:0"
		}
		args = append(args, "-map", stream)
		if step.audio {
			encoder, err := mergeAudioEncoder(plan.audio)
			if err != nil {
				return nil, err
			}
			args = append(args, encoder...)
			args = append(args, "-af", fmt.Sprintf("aresample=%d:async=1:first_pts=0,apad", plan.audio.sampleRate))
		} else {
			args = append(args, "-c:a", "copy")
		}
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-max_muxing_queue_size", "4096")
	if step.video || step.audio {
		args = append(args, "-t", strconv.FormatFloat(info.duration.Seconds(), 'f', 6, 64))
	}
	if plan.transport {
		args = append(args, "-bsf:v", "hevc_mp4toannexb", "-f", "mpegts")
	} else {
		args = append(args, "-video_track_timescale", strconv.Itoa(plan.timescale), "-f", "mp4")
	}
	return append(args, "-y", output), nil
}
