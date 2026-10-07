package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mergeFixtureCommand(t *testing.T, ffmpeg string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, append([]string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y"}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("synthetic merge fixture: %v\n%s", err, output)
	}
	return string(output)
}

func mergeFixtureFrames(t *testing.T, ffmpeg, path string) []string {
	t.Helper()
	output := mergeFixtureCommand(t, ffmpeg, "-i", path, "-map", "0:v:0", "-fps_mode", "passthrough", "-f", "framemd5", "-")
	var frames []string
	for _, line := range strings.Split(output, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		frames = append(frames, strings.TrimSpace(parts[len(parts)-1]))
	}
	return frames
}

func TestMergePreservesMajorityAndOnlyConvertsIncompatibleStreams(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg unavailable")
	}
	work := t.TempDir()
	base := filepath.Join(work, "majority.mp4")
	mergeFixtureCommand(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25:duration=2", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=2", "-c:v", "libx264", "-preset", "veryfast", "-threads:v", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", "2", base)
	variants := map[string][]string{
		"timebase":   {"-c", "copy", "-video_track_timescale", "90000"},
		"audio":      {"-c:v", "copy", "-c:a", "aac", "-ar", "44100"},
		"small":      {"-c:v", "libx264", "-preset", "veryfast", "-threads:v", "2", "-vf", "scale=240:136", "-c:a", "copy"},
		"parameters": {"-c:v", "libx264", "-preset", "fast", "-threads:v", "2", "-c:a", "copy"},
		"silent":     {"-c:v", "copy", "-an"},
		"reordered":  {"-map", "0:a:0", "-map", "0:v:0", "-c", "copy"},
	}
	for name, args := range variants {
		mergeFixtureCommand(t, ffmpeg, append(append([]string{"-i", base}, args...), filepath.Join(work, name+".mp4"))...)
	}
	baseFrames := mergeFixtureFrames(t, ffmpeg, base)
	for _, test := range []struct {
		name         string
		video, audio int
	}{
		{"timebase", 0, 0}, {"parameters", 0, 0}, {"audio", 0, 1}, {"small", 1, 0}, {"silent", 0, 1}, {"reordered", 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			minority := filepath.Join(work, test.name+".mp4")
			paths := []string{minority, base, base}
			var infos []mergeMediaInfo
			for _, path := range paths {
				info, err := probeMergeMedia(context.Background(), ffmpeg, path)
				if err != nil {
					t.Fatal(err)
				}
				infos = append(infos, info)
			}
			plan := planMergeMedia(infos)
			if plan.videoCount != test.video || plan.audioCount != test.audio || plan.video.width != 320 || plan.video.height != 180 {
				t.Fatalf("wrong majority/stream selection: %+v", plan)
			}
			output := filepath.Join(t.TempDir(), "merged.mp4")
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			method, err := mergeMediaFiles(ctx, ffmpeg, paths, output, nil)
			if err != nil {
				t.Fatal(err)
			}
			frames := mergeFixtureFrames(t, ffmpeg, output)
			if len(frames) != 150 || !reflect.DeepEqual(frames[50:100], baseFrames) || !reflect.DeepEqual(frames[100:], baseFrames) {
				t.Fatalf("majority frames changed or disappeared: frames=%d method=%s", len(frames), method)
			}
			if test.video == 0 && !reflect.DeepEqual(frames[:50], mergeFixtureFrames(t, ffmpeg, minority)) {
				t.Fatal("copied minority video changed")
			}
			merged, err := probeMergeMedia(ctx, ffmpeg, output)
			if err != nil || !merged.audio || merged.sampleRate != 48000 || merged.width != 320 || merged.height != 180 {
				t.Fatalf("output format: %+v %v", merged, err)
			}
		})
	}
	t.Run("retain-real-audio-when-majority-silent", func(t *testing.T) {
		silent := filepath.Join(work, "silent.mp4")
		output := filepath.Join(t.TempDir(), "merged.mp4")
		method, err := mergeMediaFiles(context.Background(), ffmpeg, []string{silent, base, silent}, output, nil)
		if err != nil || !strings.Contains(method, "调整音轨 2/3") || strings.Contains(method, "转视频") {
			t.Fatal("real audio was lost or silent majority video encoded", method, err)
		}
		info, err := probeMergeMedia(context.Background(), ffmpeg, output)
		if err != nil || !info.audio {
			t.Fatal(info, err)
		}
	})
	t.Run("cancel-preserves-inputs", func(t *testing.T) {
		before, _ := os.ReadFile(base)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := mergeMediaFiles(ctx, ffmpeg, []string{base, base}, filepath.Join(t.TempDir(), "out.mp4"), nil); err == nil {
			t.Fatal("canceled merge succeeded")
		}
		after, _ := os.ReadFile(base)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("merge altered original episode")
		}
	})
	t.Run("hevc-parameter-sets", func(t *testing.T) {
		encoders := mergeFixtureCommand(t, ffmpeg, "-encoders")
		if !strings.Contains(encoders, "libx265") {
			t.Skip("HEVC encoder unavailable")
		}
		hevc := filepath.Join(work, "hevc.mp4")
		mergeFixtureCommand(t, ffmpeg, "-i", base, "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "pools=1:frame-threads=1:log-level=error", "-c:a", "copy", hevc)
		for _, majority := range []string{base, hevc} {
			minority := hevc
			if majority == hevc {
				minority = filepath.Join(work, "small.mp4")
			}
			output := filepath.Join(t.TempDir(), "merged.mp4")
			method, err := mergeMediaFiles(context.Background(), ffmpeg, []string{minority, majority, majority}, output, nil)
			if err != nil || !strings.Contains(method, "仅转视频 1/3") {
				t.Fatal(method, err)
			}
			frames := mergeFixtureFrames(t, ffmpeg, output)
			want := mergeFixtureFrames(t, ffmpeg, majority)
			if len(frames) != 150 || !reflect.DeepEqual(frames[50:100], want) || !reflect.DeepEqual(frames[100:], want) {
				t.Fatalf("parameter changes corrupted copied majority frames: %d", len(frames))
			}
		}
	})
}
