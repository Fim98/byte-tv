package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDesktopWatchCatalogPlaybackResumeAndFollowing(t *testing.T) {
	ffmpeg := desktopFixtureFFmpeg(t)
	video := filepath.Join(t.TempDir(), "episode.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=2", "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=44100", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast", "-c:a", "aac", "-movflags", "+faststart", video)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate local episode: %v %s", err, output)
	}
	dataDir := t.TempDir()
	localID := "hongguo:7000000000000000001"
	otherID := "hongguo:7000000000000000002"
	localTitle := "本地完成的香火短剧"
	otherTitle := "另一部不该被搜到的剧"
	if err := writeLibraryCache(dataDir, libraryCache{Dramas: []Drama{
		{ID: localID, Source: sourceHongguo, Title: localTitle, CoverURL: "https://example.test/cover.jpg", TotalEpisode: 1},
		{ID: otherID, Source: sourceHongguo, Title: otherTitle, TotalEpisode: 2},
	}, LoadedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	state := uiState{TaskOrder: []string{"local-episode"}, Tasks: []*UITask{{
		ID: "local-episode", DramaID: localID, DramaTitle: localTitle, Episode: "1", Title: "第1集",
		Status: uiStatusSuccess, Progress: 100, Path: video, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Source: Task{DramaID: localID, DramaTitle: localTitle, Index: 1, Total: 1, OutPath: video, Chapter: Chapter{ID: localID + ":1", Source: sourceHongguo, Title: "第1集", CurrentEpisode: json.RawMessage(`1`)}},
	}}}
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ui-state.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	var networkHits atomic.Int32
	ui := NewUIApp(NewDownloader(Config{dataDir: dataDir, OutputDir: filepath.Join(dataDir, "out"), FFmpeg: ffmpeg, Concurrency: 1, settingsLoaded: true}))
	ui.downloader.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		networkHits.Add(1)
		return nil, fmt.Errorf("desktop watch test blocked a live request to %s", request.URL.Host)
	})
	t.Cleanup(ui.CloseDesktop)
	ui.enableWindowPlayback()

	client := newDesktopWatchClient(t, ui.WindowHandler())
	catalog := client.getJSON(t, "/api/desktop/catalog")
	cards := desktopJSONList(t, catalog, "data")
	if len(cards) < 2 || !desktopListHasTitle(cards, localTitle) || !desktopListHasTitle(cards, otherTitle) {
		t.Fatalf("catalog = %#v", cards)
	}
	var localCard map[string]any
	for _, card := range cards {
		if card["title"] == localTitle {
			localCard = card
		}
	}
	if cover, _ := localCard["cover"].(string); !strings.Contains(cover, "/api/ui/image") {
		t.Fatalf("library cover was not preserved: %#v", localCard)
	}
	matched := desktopJSONList(t, client.getJSON(t, "/api/desktop/catalog?q="+url.QueryEscape("香火")), "data")
	if len(matched) != 1 || matched[0]["title"] != localTitle || desktopListHasTitle(matched, otherTitle) {
		t.Fatalf("title search did not drop non-matches: %#v", matched)
	}
	detail := client.getJSON(t, "/api/desktop/drama?id="+url.QueryEscape(localID))
	episodes := desktopJSONList(t, detail, "episodes")
	if len(episodes) == 0 {
		t.Fatalf("opened drama has no episodes: %#v", detail)
	}
	taskID, _ := episodes[0]["taskId"].(string)
	if taskID == "" || episodes[0]["local"] != true {
		t.Fatalf("local episode was not listed: %#v", episodes[0])
	}

	opened := client.postJSON(t, "/api/ui/playback/open", map[string]any{"taskId": taskID})
	session, _ := opened["session"].(string)
	openedEpisodes := desktopJSONList(t, opened, "episodes")
	if session == "" || len(openedEpisodes) == 0 {
		t.Fatalf("playback open = %#v", opened)
	}
	if opened["mode"] == "collection" {
		prepared := client.postJSON(t, "/api/ui/playback/prepare", map[string]any{"session": session, "episode": 1})
		if prepared["source"] != "local" {
			t.Fatalf("local episode was not prepared from disk: %#v", prepared)
		}
	}
	plan := client.postJSON(t, "/api/ui/playback/plan", map[string]any{
		"session": session, "episode": 1, "start": 0, "quality": 0, "version": 1, "mode": "auto",
		"client": map[string]any{"mp4": true, "nativeHls": true, "hlsjs": true, "video": []string{"h264", "hevc", "av1"}, "audio": []string{"aac", "mp3", "ac3"}},
	})
	mediaURL, _ := plan["url"].(string)
	if mediaURL == "" {
		t.Fatalf("playback plan has no media url: %#v", plan)
	}
	status, header, media := client.get(t, mediaURL)
	contentType := header.Get("Content-Type")
	if status != http.StatusOK && status != http.StatusPartialContent {
		t.Fatalf("playback stream status %d %s", status, media)
	}
	if strings.Contains(contentType, "mpegurl") || bytes.Contains(media, []byte("#EXTM3U")) {
		if !desktopPlaylistHasSource(media) {
			t.Fatalf("playlist has no source: %s", media)
		}
	} else if !strings.Contains(contentType, "video") || !bytes.Contains(media, []byte("ftyp")) || len(media) < 32 {
		t.Fatalf("stream is not playable video: %s %d %q", contentType, len(media), media[:min(80, len(media))])
	}
	if plan["source"] != "local" {
		t.Fatalf("local episode was not served from the local pipeline: %#v", plan)
	}

	duration, _ := plan["duration"].(float64)
	position := 0.4
	if duration > 0 && position >= duration {
		position = duration / 2
	}
	if duration <= 0 {
		duration = 2
	}
	run := plan["run"]
	progress := client.postJSON(t, "/api/ui/playback/progress", map[string]any{
		"session":  session,
		"progress": map[string]any{"run": run, "sequence": 1, "episode": 1, "position": position, "duration": duration, "completed": false},
	})
	if progress["saved"] != true {
		t.Fatalf("progress was not saved: %#v", progress)
	}
	history := desktopJSONList(t, client.getJSON(t, "/api/ui/playback/history"), "data")
	if !desktopHistoryHas(history, localID, position) {
		t.Fatalf("history did not restore progress: %#v", history)
	}
	followed := client.postJSON(t, "/api/ui/following", map[string]any{"dramaId": localID, "saved": true})
	if followed["ok"] != true {
		t.Fatalf("following add = %#v", followed)
	}
	following := desktopJSONList(t, client.getJSON(t, "/api/ui/following"), "data")
	if !desktopListHasID(following, localID) {
		t.Fatalf("following list = %#v", following)
	}
	legacy := httptest.NewRecorder()
	ui.WindowHandler().ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
	if legacy.Code != http.StatusOK || !bytes.Contains(legacy.Body.Bytes(), []byte("/assets/main.js")) || !bytes.Contains(legacy.Body.Bytes(), []byte("搜索剧名、简介或标签")) || bytes.Contains(legacy.Body.Bytes(), []byte(`id="root"`)) {
		t.Fatalf("desktop window handler did not serve the original page: status %d", legacy.Code)
	}
	for _, path := range []string{"/assets/library.js", "/assets/app.css", "/assets/main.js"} {
		asset := httptest.NewRecorder()
		ui.WindowHandler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil))
		if asset.Code != http.StatusOK || asset.Body.Len() == 0 {
			t.Fatalf("original asset %s was not served: status %d", path, asset.Code)
		}
		if path == "/assets/library.js" && !bytes.Contains(asset.Body.Bytes(), []byte("/api/ui/dramas")) {
			t.Fatal("original library script was not served")
		}
	}
	if networkHits.Load() != 0 {
		t.Fatalf("local playback made %d live requests", networkHits.Load())
	}
}

type desktopWatchClient struct {
	base    string
	http    *http.Client
	headers http.Header
}

func newDesktopWatchClient(t *testing.T, handler http.Handler) *desktopWatchClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &desktopWatchClient{base: server.URL, http: &http.Client{Jar: jar, Timeout: 60 * time.Second}, headers: http.Header{}}
	first := client.getJSON(t, "/api/ui/viewer")
	if first["ready"] != true {
		first = client.getJSON(t, "/api/ui/viewer?confirm=1")
	}
	id, _ := first["id"].(string)
	if id == "" || first["ready"] != true {
		t.Fatalf("viewer = %#v", first)
	}
	sources, _ := first["sources"].([]any)
	joined := make([]string, 0, len(sources))
	for _, source := range sources {
		text, _ := source.(string)
		joined = append(joined, text)
	}
	online, _ := first["onlineOnly"].(bool)
	client.headers.Set("X-Juku-Viewer", id)
	client.headers.Set("X-Juku-Sources", strings.Join(joined, ","))
	client.headers.Set("X-Juku-Online-Only", fmt.Sprintf("%t", online))
	return client
}

func (client *desktopWatchClient) getJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	status, _, body := client.get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, status, body)
	}
	return decodeDesktopJSON(t, body)
}

func (client *desktopWatchClient) postJSON(t *testing.T, path string, payload any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, client.base+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, values := range client.headers {
		request.Header[key] = append([]string(nil), values...)
	}
	response, err := client.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: %d %s", path, response.StatusCode, body)
	}
	return decodeDesktopJSON(t, body)
}

func (client *desktopWatchClient) get(t *testing.T, path string) (int, http.Header, []byte) {
	t.Helper()
	target := path
	if !strings.HasPrefix(path, "http") {
		target = client.base + path
	}
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, values := range client.headers {
		request.Header[key] = append([]string(nil), values...)
	}
	response, err := client.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header.Clone(), body
}

func decodeDesktopJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json: %v %s", err, body)
	}
	return payload
}

func desktopJSONList(t *testing.T, payload map[string]any, key string) []map[string]any {
	t.Helper()
	raw, _ := payload[key].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		if entry == nil {
			t.Fatalf("%s entry is not an object: %#v", key, item)
		}
		items = append(items, entry)
	}
	return items
}

func desktopListHasTitle(items []map[string]any, title string) bool {
	for _, item := range items {
		if item["title"] == title {
			return true
		}
	}
	return false
}

func desktopListHasID(items []map[string]any, id string) bool {
	for _, item := range items {
		if item["dramaId"] == id || item["id"] == id {
			return true
		}
	}
	return false
}

func desktopHistoryHas(items []map[string]any, id string, position float64) bool {
	for _, item := range items {
		if item["dramaId"] != id {
			continue
		}
		got, _ := item["position"].(float64)
		index, _ := item["index"].(float64)
		if index == 1 && got > 0 && got+0.05 >= position && got-0.05 <= position {
			return true
		}
	}
	return false
}

func desktopPlaylistHasSource(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func desktopFixtureFFmpeg(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		candidates = append(candidates, path)
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		root := filepath.Join(filepath.Dir(file), "..", "..")
		candidates = append(candidates, filepath.Join(root, "bin", runtime.GOOS+"-"+runtime.GOARCH, "verified-b6.1.1", "ffmpeg"))
		candidates = append(candidates, filepath.Join(root, "bin", "darwin-arm64", "verified-b6.1.1", "ffmpeg"))
	}
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			return path
		}
	}
	t.Fatal("ffmpeg is required to build the local playback fixture")
	return ""
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
