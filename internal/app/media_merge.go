package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func runMergeFFmpeg(ctx context.Context, ffmpeg string, args []string, progress func(time.Duration)) error {
	command := exec.CommandContext(ctx, ffmpeg, append([]string{
		"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-progress", "pipe:1",
	}, args...)...)
	diagnostics := &cappedStringWriter{limit: 64 * 1024}
	command.Stderr = diagnostics
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	if err := command.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && key == "out_time_us" && progress != nil {
			if micros, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && micros >= 0 {
				progress(time.Duration(micros) * time.Microsecond)
			}
		}
	}
	err = command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("FFmpeg 处理失败：%w %s", err, truncate(diagnostics.String(), 1500))
	}
	return scanner.Err()
}

func mergeMediaFiles(ctx context.Context, ffmpeg string, paths []string, output string, report func(int, string)) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("没有可合并的分集")
	}
	lastProgress, lastDetail := -1, ""
	notify := func(progress int, detail string) {
		if progress > 99 {
			progress = 99
		}
		if progress < lastProgress {
			progress = lastProgress
		}
		if report != nil && (progress != lastProgress || detail != lastDetail) {
			report(progress, detail)
		}
		lastProgress, lastDetail = progress, detail
	}
	metadata := make([]mergeMediaInfo, 0, len(paths))
	var duration time.Duration
	for index, path := range paths {
		notify(index*5/len(paths), fmt.Sprintf("检测分集编码 %d/%d", index+1, len(paths)))
		info, err := probeMergeMedia(ctx, ffmpeg, path)
		if err != nil {
			return "", err
		}
		metadata = append(metadata, info)
		duration += info.duration
	}
	plan := planMergeMedia(metadata)
	method := plan.description()
	work, err := os.MkdirTemp(filepath.Dir(output), ".merge-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	inputs := append([]string(nil), paths...)
	preparedArgs := make([][]string, len(paths))
	var preparedDuration, processed time.Duration
	for index, step := range plan.steps {
		if step.video || step.audio || step.remux {
			preparedDuration += metadata[index].duration
			extension := ".mp4"
			if plan.transport {
				extension = ".ts"
			}
			inputs[index] = filepath.Join(work, fmt.Sprintf("%06d%s", index+1, extension))
			preparedArgs[index], err = plan.prepareArgs(paths[index], inputs[index], metadata[index], step)
			if err != nil {
				return "", fmt.Errorf("无法准备 %s：%w；原分集已保留", filepath.Base(paths[index]), err)
			}
		}
	}
	for index, step := range plan.steps {
		if !step.video && !step.audio && !step.remux {
			continue
		}
		action := "无损换封装"
		if step.video {
			action = "转换此集视频"
		} else if step.audio {
			action = "仅调整此集音轨"
		}
		detail := fmt.Sprintf("%s · %s %d/%d", method, action, index+1, len(paths))
		notify(5+int(processed*60/preparedDuration), detail)
		err := runMergeFFmpeg(ctx, ffmpeg, preparedArgs[index], func(elapsed time.Duration) {
			if elapsed > metadata[index].duration {
				elapsed = metadata[index].duration
			}
			notify(5+int((processed+elapsed)*60/preparedDuration), detail)
		})
		if err != nil {
			return "", fmt.Errorf("处理 %s 失败：%w；原分集已保留", filepath.Base(paths[index]), err)
		}
		processed += metadata[index].duration
	}
	var manifest strings.Builder
	manifest.WriteString("ffconcat version 1.0\n")
	for index, path := range inputs {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		if strings.ContainsAny(absolute, "\r\n") {
			return "", fmt.Errorf("分集路径包含换行符，无法合并")
		}
		fmt.Fprintf(&manifest, "file '%s'\n", strings.ReplaceAll(filepath.ToSlash(absolute), "'", "'\\''"))
		if plan.transport {
			fmt.Fprintf(&manifest, "duration %.6f\n", metadata[index].duration.Seconds())
		}
	}
	list := filepath.Join(work, "inputs.ffconcat")
	if err := os.WriteFile(list, []byte(manifest.String()), 0600); err != nil {
		return "", err
	}
	notify(65, method)
	args := []string{"-protocol_whitelist", "file,pipe", "-f", "concat", "-safe", "0", "-i", list,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-c", "copy", "-video_track_timescale", strconv.Itoa(plan.timescale), "-movflags", "+faststart"}
	if plan.parameters {
		if plan.video.videoCodec == "h264" {
			args = append(args, "-tag:v", "avc3")
		} else if plan.video.videoCodec == "hevc" {
			args = append(args, "-tag:v", "hev1")
		}
	}
	args = append(args, "-y", output)
	err = runMergeFFmpeg(ctx, ffmpeg, args, func(elapsed time.Duration) {
		notify(65+int(elapsed*15/duration), method)
	})
	if err != nil {
		return "", err
	}
	notify(80, "核对合并文件时长")
	merged, err := probeMergeMedia(ctx, ffmpeg, output)
	if err != nil {
		return "", err
	}
	tolerance := duration / 100
	if tolerance < 2*time.Second {
		tolerance = 2 * time.Second
	}
	if merged.duration < duration-tolerance || merged.duration > duration+tolerance {
		return "", fmt.Errorf("合并文件时长不符（预计 %.1f 秒，实际 %.1f 秒），原分集已保留", duration.Seconds(), merged.duration.Seconds())
	}
	notify(80, "校验完整视频和音轨（只解码，不转码）")
	err = runMergeFFmpeg(ctx, ffmpeg, []string{"-xerror", "-err_detect", "explode", "-threads", "2", "-protocol_whitelist", "file,pipe", "-i", output,
		"-map", "0:v:0", "-map", "0:a:0?", "-threads:v", "2", "-fps_mode", "passthrough", "-abort_on", "empty_output", "-f", "null", "-"}, func(elapsed time.Duration) {
		notify(80+int(elapsed*19/duration), "校验完整视频和音轨（只解码，不转码）")
	})
	if err != nil {
		return "", fmt.Errorf("合并文件解码校验失败：%w；原分集已保留", err)
	}
	return method, nil
}
