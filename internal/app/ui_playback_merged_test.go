package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergedPlaybackLocalRangeResumeWithoutEpisodes(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "6", "320x180")
	path := fixture.tasks[0].OutPath
	dramaID := fixture.tasks[0].DramaID
	id := mergedPlaybackPrefix + dramaID
	app.merges = map[string]*UIMergeState{dramaID: {DramaID: dramaID, DramaTitle: "合成全集", Status: "success", StartEpisode: 1, EndEpisode: 3, Merged: 3, OutputPath: path}}
	app.taskOrder = append([]string(nil), fixture.downloadIDs...)
	for _, taskID := range fixture.downloadIDs {
		app.removeTaskLocked(taskID)
	}
	if len(app.tasks) != 0 || app.merges[dramaID] == nil {
		t.Fatal("clearing the final episode task removed the merged file record")
	}
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.downloader.cfg.FFmpeg = filepath.Join(t.TempDir(), "ffmpeg-must-not-run")
	app.mediaResources().settings.Enabled = false
	app.mu.Lock()
	err := app.saveStateLocked()
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restored := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if _, local, err := restored.mergedPlaybackTaskLocked(id); err != nil || local != path {
		t.Fatal("restart lost merged file without episode tasks", local, err)
	}
	open := func(body any) map[string]json.RawMessage {
		t.Helper()
		result := historyJSONRequest(t, app, app.handlePlaybackOpen, "/api/ui/playback/open", body)
		if result.Code != http.StatusOK {
			t.Fatal(result.Code, result.Body.String())
		}
		var response map[string]json.RawMessage
		if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	opened := open(map[string]any{"taskId": id, "resume": true})
	var sessionID string
	_ = json.Unmarshal(opened["session"], &sessionID)
	defer app.closePlaybacks()
	var episodes []playbackEpisode
	_ = json.Unmarshal(opened["episodes"], &episodes)
	if len(episodes) != 1 || !episodes[0].Merged || episodes[0].TaskID != id || episodes[0].Danmaku {
		t.Fatal("merged file did not appear as a standalone full-length selection", string(opened["episodes"]))
	}
	preparation := historyJSONRequest(t, app, app.handlePlaybackPrepare, "/api/ui/playback/prepare", map[string]any{"session": sessionID, "episode": 1})
	if preparation.Code != http.StatusOK || !strings.Contains(preparation.Body.String(), `"source":"merged"`) || len(app.tasks) != 0 {
		t.Fatal("merged playback attempted episode downloads", preparation.Code, preparation.Body.String())
	}
	result := historyJSONRequest(t, app, app.handlePlaybackPlan, "/api/ui/playback/plan", map[string]any{"session": sessionID, "episode": 1, "version": 1, "start": 2.0,
		"client": playbackClientCapabilities{MP4: true, Video: []string{"h264"}, Audio: []string{"aac"}}})
	var plan playbackMediaPlan
	if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &plan) != nil || plan.Delivery != "local" || plan.Player != "mp4" || plan.Processing != "original" {
		t.Fatal("local merged playback depended on the online optimization flag or FFmpeg", result.Code, result.Body.String())
	}
	body, _ := os.ReadFile(path)
	for _, test := range []struct {
		method, span string
		code, length int
	}{{"GET", "bytes=0-15", 206, 16}, {"GET", "bytes=-16", 206, 16}, {"HEAD", "", 200, 0}} {
		request := httptest.NewRequest(test.method, "http://localhost"+plan.URL, nil)
		request.Header.Set("Range", test.span)
		writer := httptest.NewRecorder()
		app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, request))
		if writer.Code != test.code || writer.Body.Len() != test.length || test.span == "bytes=-16" && !bytes.Equal(writer.Body.Bytes(), body[len(body)-16:]) {
			t.Fatal("merged file did not serve local byte ranges", writer.Code, writer.Body.Len())
		}
	}
	session := app.playbacks[sessionID]
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), sessionID, playbackHistoryProgress{Run: session.run, Sequence: 1, Episode: 1, Position: 2.5, Duration: 6}); err != nil || !saved {
		t.Fatal("merged progress was not saved", saved, err)
	}
	app.closePlayback(sessionID)
	resumed := open(map[string]any{"dramaId": dramaID, "resume": true, "fromHistory": true})
	if string(resumed["initialPosition"]) != "2.5" || !bytes.Contains(resumed["episodes"], []byte(`"merged":true`)) {
		t.Fatal("merged history returned to online episodes or lost the full-file timestamp", string(resumed["initialPosition"]), string(resumed["episodes"]))
	}
	for _, scope := range []accountRecord{{OnlineOnly: true}, {Sources: []string{"no-access"}}} {
		body, _ := json.Marshal(map[string]string{"taskId": id})
		request := viewerFixtureRequest(app, httptest.NewRequest(http.MethodPost, "/api/ui/playback/open", bytes.NewReader(body)))
		request.Header.Set("Content-Type", "application/json")
		request = request.WithContext(withSourceScope(request.Context(), scope))
		writer := httptest.NewRecorder()
		app.handlePlaybackOpen(writer, request)
		if writer.Code != http.StatusForbidden {
			t.Fatal("merged playback bypassed source/download permissions", scope, writer.Code, writer.Body.String())
		}
	}
	other := app.browserViewers().acquire(viewerID("different-merged-viewer"))
	defer other.release()
	request := httptest.NewRequest(http.MethodGet, "http://localhost"+plan.URL, nil)
	writer := httptest.NewRecorder()
	app.handlePlaybackMediaAsset(writer, request.WithContext(context.WithValue(request.Context(), viewerContextKey{}, other)))
	if writer.Code != http.StatusGone {
		t.Fatal("merged playback leaked to another viewer", writer.Code)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.playbackCollectionTask(id); err == nil {
		t.Fatal("missing merged file fell back to upstream video")
	}
	if _, err := app.prepareCollectionDownload(context.Background(), id); err == nil || len(app.tasks) != 0 {
		t.Fatal("missing merged file created download tasks", err)
	}
}
