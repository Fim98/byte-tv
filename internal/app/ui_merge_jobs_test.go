package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMergeJobsSurviveRequestCancellationAndDeduplicate(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(map[bool]string{false: "curl-compatible", true: "async-page"}[async], func(t *testing.T) {
			app, session := nativePlaybackFixture(t, "2", "320x180")
			app.taskOrder = session.downloadIDs
			app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
			app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: app.downloader.cfg.FFmpeg}}
			app.cfg.SkipBytes = 1
			app.mergeMu.Lock()
			var once sync.Once
			unlock := func() { once.Do(app.mergeMu.Unlock) }
			defer func() { unlock(); app.stopMergeJobs() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body, _ := json.Marshal(map[string]any{"dramaIds": []string{historyFixtureDramaID}, "async": async})
			request := httptest.NewRequest(http.MethodPost, "/api/ui/merge", bytes.NewReader(body)).WithContext(ctx)
			writer := httptest.NewRecorder()
			returned := make(chan struct{})
			go func() { app.handleMerge(writer, request); close(returned) }()
			var job *uiMergeJob
			deadline := time.Now().Add(5 * time.Second)
			for job == nil && time.Now().Before(deadline) {
				app.mu.Lock()
				job = app.mergeJobs[historyFixtureDramaID]
				app.mu.Unlock()
				time.Sleep(time.Millisecond)
			}
			if job == nil {
				t.Fatal("merge was not queued")
			}
			cancel()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("canceled request remained tied to the merge")
			}
			if async && writer.Code != http.StatusAccepted || job.ctx.Err() != nil {
				t.Fatal("request canceled its background merge", writer.Code, job.ctx.Err())
			}
			app.mu.Lock()
			duplicate, err := app.enqueueMergesLocked([]string{historyFixtureDramaID}, true)
			app.mu.Unlock()
			if err != nil || len(duplicate) != 1 || duplicate[0] != job || job.remove {
				t.Fatal("duplicate submission changed or duplicated the original merge", err)
			}
			unlock()
			select {
			case <-job.done:
			case <-time.After(30 * time.Second):
				t.Fatal("detached merge did not finish")
			}
			if !job.result.OK {
				t.Fatal("background merge failed", job.result.Error)
			}
			if _, err := os.Stat(job.result.OutputPath); err != nil {
				t.Fatal("background output missing", err)
			}
			if _, err := os.Stat(session.tasks[0].OutPath); err != nil {
				t.Fatal("duplicate submission deleted original episodes", err)
			}
		})
	}
}

func TestMergeQueueCancelAndRestartAreExplicit(t *testing.T) {
	app := viewerTestApp(t)
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.mergeMu.Lock()
	var once sync.Once
	unlock := func() { once.Do(app.mergeMu.Unlock) }
	defer func() { unlock(); app.stopMergeJobs() }()
	app.mu.Lock()
	jobs, err := app.enqueueMergesLocked([]string{historyFixtureDramaID}, false)
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restored := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if state := restored.merges[historyFixtureDramaID]; state == nil || state.Status != "failed" || state.Error == "" {
		t.Fatal("restart left an interrupted merge permanently queued", state)
	}
	writer := historyJSONRequest(t, app, app.handleMergeCancel, "/api/ui/merge/cancel", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if writer.Code != http.StatusOK {
		t.Fatal(writer.Code, writer.Body.String())
	}
	select {
	case <-jobs[0].done:
	case <-time.After(time.Second):
		t.Fatal("queued cancellation waited for an unrelated active merge")
	}
	if jobs[0].result.OK || app.merges[historyFixtureDramaID].Status != "failed" {
		t.Fatal("canceled merge reported success")
	}
}
