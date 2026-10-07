package app

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMergedPlaybackBrowserFixture(t *testing.T) {
	root := os.Getenv("JUKU_MERGED_BROWSER_ROOT")
	if root == "" {
		t.Skip("isolated browser fixture is opt-in")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(root, "synthetic-full.mp4")
	mergeFixtureCommand(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25:duration=12", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=12", "-c:v", "libx264", "-preset", "veryfast", "-threads:v", "2", "-c:a", "aac", "-ac", "2", "-movflags", "+faststart", video)
	app := viewerTestApp(t)
	app.loadedAt, app.libraryAttempted = time.Now(), true
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.tasks = map[string]*UITask{}
	app.cond = sync.NewCond(&app.mu)
	app.merges = map[string]*UIMergeState{historyFixtureDramaID: {DramaID: historyFixtureDramaID, DramaTitle: "本地合成全集", Status: "success", Progress: 100, StartEpisode: 1, EndEpisode: 3, Merged: 3, OutputPath: video}}
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
	app.cfg.SkipBytes = 1
	for index, id := range []string{"synthetic-one", "synthetic-two"} {
		task := Task{DramaID: "hongguo:7000000000000000005", DramaTitle: "待合并的本地分集", Index: index + 1, Total: 2, OutPath: video, Chapter: Chapter{Source: sourceHongguo, CurrentEpisode: rawEpisode(index + 1)}}
		app.tasks[id] = &UITask{ID: id, DramaID: task.DramaID, DramaTitle: task.DramaTitle, Path: video, Status: uiStatusSuccess, Source: task}
		app.taskOrder = append(app.taskOrder, id)
	}
	app.cfg.adminUsername, app.cfg.adminPassword, app.cfg.adminPasswordExplicit = "admin", accountFixturePassword, true
	if err := app.prepareBrowserViewers(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.embySyncer().stop)
	t.Cleanup(app.stopMergeJobs)
	server := httptest.NewServer(app.routes())
	defer server.Close()
	body, _ := json.Marshal(map[string]string{"url": server.URL})
	if err := os.WriteFile(filepath.Join(root, "ready.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
				return
			}
		case <-deadline.C:
			t.Fatal("merged playback browser fixture timed out")
		}
	}
}
