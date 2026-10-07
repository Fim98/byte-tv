package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlaybackBrowserFixture(t *testing.T) {
	root := os.Getenv("JUKU_PLAYBACK_BROWSER_ROOT")
	if root == "" {
		t.Skip("isolated browser fixture is opt-in")
	}
	mediaDir := os.Getenv("JUKU_PLAYBACK_BROWSER_MEDIA")
	if mediaDir == "" {
		t.Fatal("synthetic fixture directory required")
	}
	if _, err := os.Stat(filepath.Join(mediaDir, "original.mp4")); err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	processLog := filepath.Join(root, "ffmpeg-calls.txt")
	wrapper := filepath.Join(root, "ffmpeg-fixture")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> "+quote(processLog)+"\nexec "+quote(ffmpeg)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var blockClient atomic.Bool
	var requests []map[string]string
	files := http.FileServer(http.Dir(mediaDir))
	mediaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, map[string]string{"path": r.URL.Path, "method": r.Method, "range": r.Header.Get("Range")})
		mu.Unlock()
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Juku-Viewer") != "" {
			t.Error("site credentials reached the synthetic media origin")
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges")
		if blockClient.Load() && r.Header.Get("Sec-Fetch-Site") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path == "/redirect.m3u8" {
			http.Redirect(w, r, "/ts/index.m3u8", http.StatusFound)
			return
		}
		if r.URL.Path == "/denied.mp4" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer mediaServer.Close()
	mediaOrigin := strings.Replace(mediaServer.URL, "127.0.0.1", "localhost", 1)
	app := viewerTestApp(t)
	app.loadedAt = time.Now()
	app.tasks = map[string]*UITask{}
	app.cond = sync.NewCond(&app.mu)
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.downloader.cfg.FFmpeg, app.cfg.FFmpeg = wrapper, wrapper
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: wrapper}}
	transport := mediaServer.Client().Transport
	app.downloader.client = &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(request.URL.String(), mediaOrigin+"/") {
			t.Error("playback fixture attempted an external request", request.URL.Host)
			return nil, errors.New("external requests forbidden")
		}
		return transport.RoundTrip(request)
	})}
	app.cfg.adminUsername, app.cfg.adminPassword, app.cfg.adminPasswordExplicit = "admin", accountFixturePassword, true
	if err := app.prepareBrowserViewers(); err != nil {
		t.Fatal(err)
	}
	client := app.downloader.hongguoClient()
	entry := client.details["7000000000000000001"]
	entry.Chapters = nil
	for index, path := range []string{"/original.mp4", "/redirect.m3u8", "/fmp4/index.m3u8", "/qualities/master.m3u8", "/ac3.mp4", "/denied.mp4", "/original.mp4"} {
		entry.Chapters = append(entry.Chapters, Chapter{ID: providerChapterID(sourceHongguo, "7000000000000000001", fmt.Sprint(index+1)), Source: sourceHongguo, CurrentEpisode: rawEpisode(index + 1), VideoURL: mediaOrigin + path})
	}
	entry.Drama.Title, entry.Drama.TotalEpisode, entry.Drama.ReleaseStatus = "本机合成播放样本", len(entry.Chapters), "finished"
	client.details["7000000000000000001"] = entry
	app.dramas = []Drama{entry.Drama}
	task := Task{DramaID: entry.Drama.ID, DramaTitle: entry.Drama.Title, Index: 1, Total: 1, OutPath: filepath.Join(mediaDir, "original.mp4"), Chapter: entry.Chapters[0]}
	app.taskOrder = []string{"local-fixture"}
	app.tasks["local-fixture"] = &UITask{ID: "local-fixture", DramaID: task.DramaID, DramaTitle: task.DramaTitle, Status: uiStatusSuccess, Path: task.OutPath, Source: task}
	t.Cleanup(app.embySyncer().stop)
	routes := app.routes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fixture/status":
			mu.Lock()
			snapshot := append([]map[string]string(nil), requests...)
			mu.Unlock()
			body, _ := os.ReadFile(processLog)
			count := 0
			if len(body) > 0 {
				count = len(strings.Split(strings.TrimSpace(string(body)), "\n"))
			}
			resources := app.mediaResources()
			resources.mu.Lock()
			active := make(map[string]int)
			for kind, value := range resources.active {
				active[kind] = value
			}
			used := resources.cacheBytes
			resources.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"ffmpegCalls": count, "requests": snapshot, "active": active, "cacheBytes": used})
			return
		case "/fixture/direct-access":
			var input struct {
				Blocked bool `json:"blocked"`
			}
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			blockClient.Store(input.Blocked)
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		case "/fixture/expire":
			app.closePlaybacks()
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		case "/api/ui/cover/repair":
			writeJSON(w, http.StatusOK, map[string]any{"dramaId": historyFixtureDramaID, "retryAfter": 300})
			return
		case "/api/ui/image":
			t.Error("playback fixture must never request source images")
			http.NotFound(w, r)
			return
		}
		routes.ServeHTTP(w, r)
	}))
	defer server.Close()
	ready, _ := json.Marshal(map[string]string{"url": server.URL, "mediaOrigin": mediaOrigin, "adminPassword": accountFixturePassword})
	if err := os.WriteFile(filepath.Join(root, "ready.json"), ready, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(8 * time.Minute)
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
			t.Fatal("isolated playback browser fixture timed out")
		}
	}
}
