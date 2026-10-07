package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestIncompleteGeneratedPlaybackFallsBackAndReleasesCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	app, _ := nativePlaybackFixture(t, "3", "160x90")
	work := t.TempDir()
	path := filepath.Join(work, "ffmpeg-fixture")
	directoryLog := filepath.Join(work, "output-directory")
	t.Setenv("JUKU_TEST_OUTPUT_DIRECTORY", directoryLog)
	script := "#!/bin/sh\n" +
		"case \"$PWD\" in */juku-playback-media-*) ;; *) exit 23;; esac\n" +
		"printf '%s' \"$PWD\" > \"$JUKU_TEST_OUTPUT_DIRECTORY\"\n" +
		"printf 'synthetic-segment' > 000000.m4s\n" +
		"printf '#EXTM3U\\n#EXT-X-MAP:URI=\"init.mp4\"\\n#EXTINF:3.0,\\n000000.m4s\\n#EXT-X-ENDLIST\\n' > index.m3u8\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: path}}
	plan := plannedFixture(t, app, 1, 1, "compatible")
	if plan.Player != "legacy" || plan.Reason != "generated_output_incomplete" {
		t.Fatalf("incomplete output remained a terminal preparation error: %+v", plan)
	}
	body, err := os.ReadFile(directoryLog)
	if err != nil || !strings.HasPrefix(filepath.Base(string(body)), "juku-playback-media-") {
		t.Fatal("fixture did not run in the isolated output directory", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := os.Stat(string(body))
		resources := app.mediaResources()
		resources.mu.Lock()
		remaining := resources.cacheBytes
		resources.mu.Unlock()
		if os.IsNotExist(err) && remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fallback leaked generated files or cache quota", remaining, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	media := &playbackMediaSession{ctx: ctx, cancel: cancel, plan: playbackMediaPlan{}}
	if err := app.planMedia(ctx, media, playbackClientCapabilities{HlsJS: true}, "compatible", sourceHongguo, "", "/media/", ""); err == nil || media.plan.Reason == "generated_output_incomplete" {
		t.Fatal("cancellation incorrectly selected a second playback path", err)
	}
}
