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

func TestMergeRealHEAACAudioPreservesVideo(t *testing.T) {
	fixture := os.Getenv("JUKU_MERGE_HEAAC_FIXTURE")
	if fixture == "" {
		t.Skip("set JUKU_MERGE_HEAAC_FIXTURE to a local HE-AAC or HE-AACv2 sample; no network downloads")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	base, minority := filepath.Join(work, "he-aac.mp4"), filepath.Join(work, "lc.mp4")
	mergeFixtureCommand(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25:duration=2", "-i", fixture,
		"-map", "0:v:0", "-map", "1:a:0", "-t", "2", "-c:v", "libx264", "-preset", "veryfast", "-threads:v", "2", "-pix_fmt", "yuv420p", "-c:a", "copy", base)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	info, err := probeMergeMedia(ctx, ffmpeg, base)
	if err != nil || !strings.HasPrefix(info.audioProfile, "HE-AAC") {
		t.Fatalf("fixture is not real HE-AAC audio: %s %v", info.audioProfile, err)
	}
	mergeFixtureCommand(t, ffmpeg, "-i", base, "-c:v", "copy", "-c:a", "aac", "-profile:a", "aac_low", minority)
	baseFrames := mergeFixtureFrames(t, ffmpeg, base)
	for _, mixed := range []bool{false, true} {
		name := "homogeneous"
		paths := []string{base, base, base}
		profile := info.audioProfile
		if mixed {
			name, paths[1], profile = "mixed", minority, "LC"
		}
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(work, name+"-merged.mp4")
			method, err := mergeMediaFiles(ctx, ffmpeg, paths, output, nil)
			if err != nil || strings.Contains(method, "转视频") || strings.Contains(method, "统一音轨为 AAC-LC") != mixed {
				t.Fatal("real HE-AAC merge failed or unnecessarily encoded video", method, err)
			}
			merged, err := probeMergeMedia(ctx, ffmpeg, output)
			if err != nil || merged.audioProfile != profile || merged.sampleRate != info.sampleRate {
				t.Fatalf("unexpected merged audio: %+v %v", merged, err)
			}
			want := append(append(append([]string(nil), baseFrames...), baseFrames...), baseFrames...)
			if frames := mergeFixtureFrames(t, ffmpeg, output); !reflect.DeepEqual(frames, want) {
				t.Fatalf("copied video changed: got %d frames, want %d", len(frames), len(want))
			}
		})
	}
}

func TestMergeMixedHEAACUsesUniformLCAudioAndCopiesVideo(t *testing.T) {
	for _, profile := range []string{"HE-AAC", "HE-AACv2"} {
		t.Run(profile, func(t *testing.T) {
			base := mergeMediaInfo{width: 1080, height: 1920, videoCodec: "h264", videoProfile: "High",
				pixelFormat: "yuv420p", sar: "1/1", videoTB: "1/90000", videoExtra: "unchanged-video",
				audio: true, audioCodec: "aac", audioProfile: profile, audioExtra: "he-configuration",
				sampleRate: 48000, layout: "stereo", audioTB: "1/48000", duration: time.Minute}
			minority := base
			minority.audioProfile, minority.audioExtra = "LC", "lc-configuration"
			infos := []mergeMediaInfo{base, minority, base}
			plan := planMergeMedia(infos)
			if plan.videoCount != 0 || plan.audioCount != len(infos) || !plan.audioFallback || plan.audio.audioProfile != "LC" {
				t.Fatalf("mixed HE-AAC was not normalized without video encoding: %+v", plan)
			}
			for index, step := range plan.steps {
				args, err := plan.prepareArgs("input.mp4", "output.mp4", infos[index], step)
				joined := strings.Join(args, " ")
				if err != nil || !strings.Contains(joined, "-c:v copy") || !strings.Contains(joined, "-profile:a aac_low") ||
					strings.Contains(joined, "libx264") || strings.Contains(joined, "-vf") {
					t.Fatal("audio fallback encoded video or kept mixed AAC profiles", joined, err)
				}
			}
			same := planMergeMedia([]mergeMediaInfo{base, base, base})
			if same.audioCount != 0 || same.videoCount != 0 || same.audioFallback {
				t.Fatal("homogeneous HE-AAC should stay lossless", same.description())
			}
			minority.audio = false
			if silent := planMergeMedia([]mergeMediaInfo{base, minority, base}); silent.audioCount != 3 || silent.videoCount != 0 {
				t.Fatal("silent episode did not normalize the full audio track")
			}
		})
	}
}

func TestMergeFailureRetainsProgress(t *testing.T) {
	app := &UIApp{statePath: filepath.Join(t.TempDir(), "ui-state.json")}
	result := uiMergeResult{DramaID: historyFixtureDramaID, DramaTitle: "合成进度测试"}
	app.setMergeState(result, "running", 41, false, false)
	result.Error = "fixture failure"
	app.setMergeState(result, "failed", 0, false, false)
	if state := app.merges[result.DramaID]; state.Progress != 41 || state.Status != "failed" || state.Error == "" {
		t.Fatal("failure reset the completed work", state)
	}
	app.setMergeState(result, "running", 1, false, false)
	if app.merges[result.DramaID].Progress != 1 {
		t.Fatal("a new attempt inherited the previous progress")
	}
}
