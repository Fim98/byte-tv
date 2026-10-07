package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlaybackDirectHLSChecksCORSKeysAndScheme(t *testing.T) {
	app, _ := prefetchFixtureApp(t)
	var cors atomic.Bool
	cors.Store(true)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "" || r.Header.Get("X-Juku-Viewer") != "" {
			t.Error("direct eligibility depended on server credentials")
		}
		if r.URL.Path != "/part.ts" || cors.Load() {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		if r.URL.Path == "/index.m3u8" {
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"public.key\"\n#EXTINF:3,\npart.ts\n#EXT-X-ENDLIST\n")
		}
	}))
	defer upstream.Close()
	app.downloader.client = upstream.Client()
	app.mediaResources().settings.DirectSources = []string{sourceHongguo}
	media := &playbackMediaSession{media: providerMedia{URL: upstream.URL + "/index.m3u8"}, plan: playbackMediaPlan{Player: "hls"}}
	if !app.allowPlaybackDirect(context.Background(), media, sourceHongguo, "http://library.test") || requests.Load() != 4 {
		t.Fatal("public HLS resources were not all checked", requests.Load())
	}
	cors.Store(false)
	if app.allowPlaybackDirect(context.Background(), media, sourceHongguo, "http://library.test") {
		t.Fatal("segment without CORS was allowed")
	}
	cors.Store(true)
	before := requests.Load()
	if app.allowPlaybackDirect(context.Background(), media, sourceHongguo, "https://library.test") {
		t.Fatal("HTTPS page was allowed to redirect to HTTP media")
	}
	media.key = make([]byte, 16)
	if app.allowPlaybackDirect(context.Background(), media, sourceHongguo, "http://library.test") || requests.Load() != before {
		t.Fatal("server-only key was treated as public")
	}
	media.key = nil
	for source, group := range accountSourceAliases {
		app.mediaResources().settings.DirectSources = []string{group}
		if !app.allowPlaybackDirect(context.Background(), media, source, "http://library.test") {
			t.Fatal("source alias ignored the administrator setting", source)
		}
	}
}

func TestPlaybackOriginalHLSReloadsGrowingPlaylist(t *testing.T) {
	app, _ := prefetchFixtureApp(t)
	var sequence atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next := sequence.Add(1)
		fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXTINF:3,\npart-%d.ts\n", next, next)
	}))
	defer upstream.Close()
	app.downloader.client = upstream.Client()
	ctx, cancel := context.WithCancel(context.Background())
	media := &playbackMediaSession{ctx: ctx, cancel: cancel, media: providerMedia{URL: upstream.URL + "/live.m3u8", Playlist: "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:3,\nold.ts\n"}, plan: playbackMediaPlan{Player: "hls"}}
	defer media.Close()
	app.attachMediaGateway(media, "/media/", "")
	for expected := 1; expected <= 2; expected++ {
		writer := httptest.NewRecorder()
		app.serveMediaSession(writer, httptest.NewRequest(http.MethodGet, "http://localhost"+media.plan.URL, nil), media)
		if writer.Code != 200 || !strings.Contains(writer.Body.String(), fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d", expected)) || strings.Contains(writer.Body.String(), "#EXT-X-ENDLIST") {
			t.Fatal("growing HLS was frozen or declared complete", writer.Code, writer.Body.String())
		}
	}
}

func TestPlaybackPlanRefusalAndMetadataOnlyPrefetch(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "3", "160x90")
	body, err := os.ReadFile(fixture.tasks[0].OutPath)
	if err != nil {
		t.Fatal(err)
	}
	var refused atomic.Bool
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if refused.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, "fixture.mp4", time.Time{}, bytes.NewReader(body))
	}))
	defer upstream.Close()
	app.downloader.client = upstream.Client()
	plannedFixture(t, app, 1, 1, "auto")
	fixture.downloadIDs = nil
	fixture.tasks[1].Chapter.Source = sourceHongguo
	fixture.tasks[1].Chapter.VideoURL = upstream.URL + "/next.mp4"
	result := prefetchRequest(app, fmt.Sprintf(`{"session":"fixture","episode":2,"run":%d,"version":1}`, fixture.run))
	if result.Code != http.StatusAccepted {
		t.Fatal(result.Code, result.Body.String())
	}
	cache := fixture.prefetch
	select {
	case <-cache.done:
	case <-time.After(3 * time.Second):
		t.Fatal("address prefetch did not finish")
	}
	if view := cache.view(); view.State != "resolved" || requests.Load() != 0 || app.downloader.ffmpegInstaller != nil {
		t.Fatal("address prefetch transferred media or encoded video", view, requests.Load())
	}
	fixture.tasks[1].Chapter.VideoURL = ""
	plan := plannedFixture(t, app, 2, 2, "auto")
	if plan.Processing != "original" || fixture.media.media.URL != upstream.URL+"/next.mp4" {
		t.Fatal("prepared address was not reused with a nonzero resume position", plan)
	}
	refused.Store(true)
	writer := httptest.NewRecorder()
	app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost"+plan.URL, nil)))
	state, _ := app.playbackStatus("fixture", false)
	if writer.Code != 403 || !state.MediaFailure || app.downloader.ffmpegInstaller != nil {
		t.Fatal("upstream refusal was hidden or started FFmpeg", writer.Code, state)
	}
}

func TestPlaybackPlanConvertsOnlyUnsupportedAudio(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "3", "160x90")
	ffmpeg := app.downloader.cfg.FFmpeg
	original := fixture.tasks[0].OutPath
	input := filepath.Join(t.TempDir(), "ac3.mp4")
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", original, "-c:v", "copy", "-c:a", "ac3", input)
	if body, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("AC3 fixture: %v %s", err, body)
	}
	fixture.tasks[0].OutPath = input
	app.tasks["one"].Path, app.tasks["one"].Source.OutPath = input, input
	app.mediaResources().settings.MaxVideoTranscodes = 0
	plan := plannedFixture(t, app, 1, 1, "auto")
	if plan.Processing != "audio" || plan.Player != "hls" || plan.Reason != "audio_codec_unsupported" {
		t.Fatal("unsupported audio did not select audio-only processing", plan)
	}
	media := fixture.media
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { app.serveMediaSession(w, r, media) }))
	defer server.Close()
	if !reflect.DeepEqual(playbackDecodedFrames(t, ffmpeg, original), playbackDecodedFrames(t, ffmpeg, server.URL+plan.URL)) {
		t.Fatal("audio compatibility conversion changed video frames")
	}
	other, err := app.resolveMediaSession(context.Background(), context.Background(), fixture.tasks[1], "two", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := app.planMedia(context.Background(), other, playbackClientCapabilities{MP4: true, NativeHLS: true, Video: []string{"hevc"}, Audio: []string{"aac"}}, "auto", sourceHongguo, "", "/media/", ""); err != nil || other.plan.Player != "legacy" || other.plan.Reason != "video_codec_unsupported" {
		t.Fatal("unsupported video did not choose the final compatibility fallback", other.plan, err)
	}
}

func TestPlaybackGeneratedCacheQuotaAndCleanup(t *testing.T) {
	app, _ := prefetchFixtureApp(t)
	resources := app.mediaResources()
	resources.settings.CacheMB = 64
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	generated := &playbackGenerated{ctx: ctx, cancel: cancel, directory: directory, resources: resources, done: make(chan struct{})}
	close(generated.done)
	defer generated.Close()
	for index := 0; index < 2; index++ {
		file, err := os.Create(filepath.Join(directory, fmt.Sprintf("part-%d.m4s", index)))
		if err != nil {
			t.Fatal(err)
		}
		err = file.Truncate(40 << 20)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		err = generated.accountCache()
		if (err == nil) != (index == 0) {
			t.Fatal("cache quota did not enforce the combined size", index, err)
		}
	}
	generated.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resources.mu.Lock()
		used := resources.cacheBytes
		resources.mu.Unlock()
		if used == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed cache retained its quota", used)
		}
		time.Sleep(time.Millisecond)
	}
}
