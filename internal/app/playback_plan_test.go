package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func plannedFixture(t *testing.T, app *UIApp, episode, version int, mode string) playbackMediaPlan {
	t.Helper()
	app.mediaResources().settings.Enabled = true
	body, _ := json.Marshal(map[string]any{"session": "fixture", "episode": episode, "version": version, "start": 1.25, "mode": mode,
		"client": playbackClientCapabilities{MP4: true, NativeHLS: true, HlsJS: true, Video: []string{"h264"}, Audio: []string{"aac"}}})
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/ui/playback/plan", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost")
	writer := httptest.NewRecorder()
	app.handlePlaybackPlan(writer, viewerFixtureRequest(app, request))
	if writer.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", writer.Code, writer.Body.String())
	}
	var plan playbackMediaPlan
	if err := json.Unmarshal(writer.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPlaybackPlanLocalMP4WithoutFFmpegAndVersionIsolation(t *testing.T) {
	app, session := nativePlaybackFixture(t, "4", "320x180")
	app.downloader.cfg.FFmpeg = filepath.Join(t.TempDir(), "unavailable-ffmpeg")
	plan := plannedFixture(t, app, 1, 1, "auto")
	if plan.Player != "mp4" || plan.Delivery != "local" || plan.Processing != "original" || plan.Duration < 3.9 || plan.Duration > 4.1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	body, err := os.ReadFile(session.tasks[0].OutPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, span string
		code, length int
	}{{"GET", "bytes=0-15", 206, 16}, {"HEAD", "", 200, 0}, {"GET", "bytes=-16", 206, 16}, {"GET", "bytes=999999999-", 416, -1}} {
		request := httptest.NewRequest(test.method, "http://localhost"+plan.URL, nil)
		request.Header.Set("Range", test.span)
		writer := httptest.NewRecorder()
		app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, request))
		if writer.Code != test.code || test.length >= 0 && writer.Body.Len() != test.length {
			t.Fatalf("media %s %s: %d %d", test.method, test.span, writer.Code, writer.Body.Len())
		}
		if test.span == "bytes=-16" && !bytes.Equal(writer.Body.Bytes(), body[len(body)-16:]) {
			t.Fatal("tail range changed bytes")
		}
	}
	if app.downloader.ffmpegInstaller != nil {
		t.Fatal("original playback initialized FFmpeg")
	}
	other := app.browserViewers().acquire(viewerID("another-fixture-viewer"))
	defer other.release()
	writer := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://localhost"+plan.URL, nil)
	app.handlePlaybackMediaAsset(writer, request.WithContext(context.WithValue(request.Context(), viewerContextKey{}, other)))
	if writer.Code != http.StatusGone {
		t.Fatal("another viewer obtained media", writer.Code)
	}
	old := session.media
	plannedFixture(t, app, 2, 2, "auto")
	writer = httptest.NewRecorder()
	app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, request))
	if writer.Code != http.StatusGone || old.ctx.Err() == nil {
		t.Fatal("switching episodes kept the previous media active")
	}
}

func TestPlaybackPlanRemoteRangeRedirectAndFallback(t *testing.T) {
	app, session := nativePlaybackFixture(t, "4", "320x180")
	body, _ := os.ReadFile(session.tasks[0].OutPath)
	var mediaRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mediaRequests.Add(1)
		if request.Header.Get("X-Juku-Viewer") != "" || request.Header.Get("Cookie") != "" || request.Header.Get("Authorization") != "" {
			t.Error("site credentials forwarded upstream")
		}
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("ETag", `"fixture-v1"`)
		if request.URL.Path == "/no-range.mp4" {
			writer.Write(body)
			return
		}
		http.ServeContent(writer, request, "fixture.mp4", time.Time{}, bytes.NewReader(body))
	}))
	defer upstream.Close()
	app.downloader.client = upstream.Client()
	session.downloadIDs = nil
	session.tasks[0].Chapter.Source, session.tasks[0].Chapter.VideoURL = sourceHongguo, upstream.URL+"/fixture.mp4"
	app.mediaResources().settings.DirectSources = []string{sourceHongguo}
	plan := plannedFixture(t, app, 1, 1, "auto")
	if plan.Delivery != "redirect" {
		t.Fatalf("eligible MP4 did not redirect: %+v", plan)
	}
	before := mediaRequests.Load()
	writer := httptest.NewRecorder()
	app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost"+plan.URL, nil)))
	if writer.Code != http.StatusFound || writer.Header().Get("Location") != upstream.URL+"/fixture.mp4" || writer.Body.Len() != 0 || mediaRequests.Load() != before {
		t.Fatal("redirect transferred media through the app")
	}
	plan = plannedFixture(t, app, 1, 2, "proxy")
	if plan.Delivery != "proxy" || plan.Processing != "original" {
		t.Fatal("direct failure selected encoding")
	}
	for _, span := range []string{"bytes=0-15", "bytes=-16", "bytes=999999999-"} {
		request := httptest.NewRequest(http.MethodGet, "http://localhost"+plan.URL, nil)
		request.Header.Set("Range", span)
		writer = httptest.NewRecorder()
		app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, request))
		if span == "bytes=999999999-" {
			if writer.Code != 416 || writer.Header().Get("Content-Range") == "" {
				t.Fatal("range error was hidden", writer.Code)
			}
		} else if writer.Code != 206 || writer.Body.Len() != 16 {
			t.Fatal("remote range corrupted", writer.Code, writer.Body.Len())
		}
	}
	session.tasks[0].Chapter.VideoURL = upstream.URL + "/no-range.mp4"
	plan = plannedFixture(t, app, 1, 3, "auto")
	request := httptest.NewRequest(http.MethodGet, "http://localhost"+plan.URL, nil)
	request.Header.Set("Range", "bytes=9-15")
	writer = httptest.NewRecorder()
	app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, request))
	if plan.Delivery != "proxy" || writer.Code != 200 || writer.Header().Get("Accept-Ranges") != "" || !bytes.Equal(writer.Body.Bytes(), body) {
		t.Fatal("proxy invented unsupported ranges")
	}
	if app.downloader.ffmpegInstaller != nil {
		t.Fatal("raw remote media invoked FFmpeg")
	}
}

func playbackDecodedFrames(t *testing.T, ffmpeg, address string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "1", "-i", address, "-map", "0:v:0", "-an", "-fps_mode", "passthrough", "-f", "framemd5", "-").Output()
	if err != nil {
		t.Fatal("decode fixture", err)
	}
	var frames []string
	for _, line := range strings.Split(string(output), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			fields := strings.Split(line, ",")
			frames = append(frames, strings.TrimSpace(fields[len(fields)-1]))
		}
	}
	return frames
}

func TestPlaybackGeneratedInitializationPathMatchesPlaylist(t *testing.T) {
	args := playbackGeneratedArgs(providerMedia{}, "input.mp4", "remux")
	playlist := args[len(args)-1]
	var init, segment string
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-hls_fmp4_init_filename":
			init = args[i+1]
		case "-hls_segment_filename":
			segment = args[i+1]
		}
	}
	if init != "init.mp4" || playlist != "index.m3u8" || segment != "%06d.m4s" {
		t.Fatalf("FFmpeg output paths are not portable: %q", args)
	}
}

func TestPlaybackGeneratedPreservesLongGOPAndAudioOnlyVideo(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "10", "320x180")
	ffmpeg := app.downloader.cfg.FFmpeg
	source := filepath.Join(t.TempDir(), "gop3.mp4")
	command := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", fixture.tasks[0].OutPath, "-c:v", "libx264", "-preset", "veryfast", "-g", "90", "-keyint_min", "90", "-sc_threshold", "0", "-threads", "1", "-c:a", "copy", source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture GOP: %v %s", err, output)
	}
	original := playbackDecodedFrames(t, ffmpeg, source)
	for _, mode := range []string{"remux", "audio", "cenc"} {
		t.Run(mode, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			media := &playbackMediaSession{ctx: parent, cancel: cancel, local: source, plan: playbackMediaPlan{Delivery: "local"}}
			processing := mode
			if mode == "cenc" {
				processing = "remux"
				key := "000102030405060708090a0b0c0d0e0f"
				media.media.CENCKey, _ = hex.DecodeString(key)
				media.local = filepath.Join(t.TempDir(), "encrypted.mp4")
				cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", source, "-c", "copy", "-encryption_scheme", "cenc-aes-ctr", "-encryption_key", key, "-encryption_kid", "101112131415161718191a1b1c1d1e1f", media.local)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("CENC fixture: %v %s", err, output)
				}
			}
			ctx, stop := context.WithTimeout(parent, 30*time.Second)
			defer stop()
			if err := app.generatePlayback(ctx, media, processing, "/media/", ""); err != nil {
				media.Close()
				t.Fatal(err)
			}
			defer media.Close()
			stop()
			if media.generated.ctx.Err() != nil {
				t.Fatal("completed media inherited its preparation timeout")
			}
			parts := media.generated.segments
			if len(parts) != 4 || parts[0].Duration < 2.99 || parts[0].Duration > 3.01 || parts[1].Start != parts[0].Duration || !strings.Contains(media.generated.playlist, "#EXT-X-TARGETDURATION:3") {
				t.Fatalf("invented fixed two-second timeline: %+v", parts)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { app.serveMediaSession(w, r, media) }))
			defer server.Close()
			copied := playbackDecodedFrames(t, ffmpeg, server.URL+media.plan.URL)
			if len(original) != 300 || !reflect.DeepEqual(original, copied) {
				t.Fatalf("video changed during %s: %d / %d frames", mode, len(original), len(copied))
			}
			directory := media.generated.directory
			media.Close()
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(directory); os.IsNotExist(err) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("generated media survived close")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
	app.mediaResources().mu.Lock()
	defer app.mediaResources().mu.Unlock()
	if app.mediaResources().cacheBytes != 0 {
		t.Fatal("cache budget was not released")
	}
}

func TestPlaybackOriginalHLSURIHeadersAndByteRanges(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "2", "160x90")
	var segmentRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Juku-Viewer") != "" {
			t.Error("viewer leaked to HLS origin")
		}
		switch r.URL.Path {
		case "/redirect.m3u8":
			http.Redirect(w, r, "/real/variant.m3u8", 302)
		case "/real/variant.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			io.WriteString(w, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:17\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"enc.key\",IV=0x00000000000000000000000000000011\n#EXTINF:3.123,\n#EXT-X-BYTERANGE:188@0\npart.bin\n#EXT-X-DISCONTINUITY\n#EXTINF:2.5,\n#EXT-X-BYTERANGE:188@188\npart.bin\n#EXT-X-ENDLIST\n")
		case "/real/enc.key":
			w.Write(bytes.Repeat([]byte{7}, 16))
		case "/real/init.mp4":
			w.Write([]byte("synthetic-init"))
		case "/real/part.bin":
			segmentRequests.Add(1)
			http.ServeContent(w, r, "part", time.Time{}, bytes.NewReader(bytes.Repeat([]byte{0x47}, 376)))
		default:
			t.Error("wrong redirected HLS base", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	app.downloader.client = upstream.Client()
	fixture.downloadIDs = nil
	fixture.tasks[0].Chapter.Source, fixture.tasks[0].Chapter.VideoURL = sourceHongguo, upstream.URL+"/redirect.m3u8"
	plan := plannedFixture(t, app, 1, 1, "auto")
	if plan.Player != "hls" || plan.Processing != "original" || app.downloader.ffmpegInstaller != nil {
		t.Fatalf("original HLS was processed: %+v", plan)
	}
	writer := httptest.NewRecorder()
	app.handlePlaybackMediaAsset(writer, viewerFixtureRequest(app, httptest.NewRequest("GET", "http://localhost"+plan.URL, nil)))
	playlist := writer.Body.String()
	for _, expected := range []string{"#EXT-X-MEDIA-SEQUENCE:17", "#EXTINF:3.123,", "#EXT-X-BYTERANGE:188@188", "#EXT-X-DISCONTINUITY", "#EXT-X-ENDLIST", "IV=0x00000000000000000000000000000011"} {
		if !strings.Contains(playlist, expected) {
			t.Fatal("lost HLS semantics", expected, playlist)
		}
	}
	if strings.Contains(playlist, upstream.URL) {
		t.Fatal("gateway exposed origin URLs")
	}
	for _, line := range strings.Split(playlist, "\n") {
		if !strings.HasPrefix(line, "/api/") {
			continue
		}
		request := httptest.NewRequest("GET", "http://localhost"+line, nil)
		request.Header.Set("Range", "bytes=188-375")
		part := httptest.NewRecorder()
		app.handlePlaybackMediaAsset(part, viewerFixtureRequest(app, request))
		if part.Code != 206 || part.Body.Len() != 188 {
			t.Fatal("HLS byte range changed", part.Code, part.Body.String())
		}
	}
	if segmentRequests.Load() != 2 {
		t.Fatal("unexpected HLS media fetch count", segmentRequests.Load())
	}
}

func TestPlaybackResourceBudgetsCancellationAndPersistence(t *testing.T) {
	app, admin := administratorFixture(t)
	settings := defaultPlaybackSettings()
	settings.Enabled = true
	settings.MaxVideoTranscodes, settings.MaxMediaRequests = 0, 1
	response := admin.request(t, http.MethodPost, "/api/ui/admin/playback", settings)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	restarted := &UIApp{cfg: app.cfg}
	saved := restarted.mediaResources().snapshot()
	if !saved.Enabled || saved.MaxVideoTranscodes != 0 {
		t.Fatal("explicit playback settings did not persist")
	}
	resources := app.mediaResources()
	if _, err := resources.acquire(context.Background(), "video", false); err == nil {
		t.Fatal("disabled video processing was admitted")
	}
	release, err := resources.acquire(context.Background(), "media", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := resources.acquire(ctx, "media", false); done <- err }()
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal("queued media was not canceled", err)
	}
	if _, err := resources.acquire(context.Background(), "media", true); err == nil {
		t.Fatal("background work displaced foreground playback")
	}
	release()
	release()
	resources.mu.Lock()
	if resources.active["media"] != 0 || resources.waiting["media"] != 0 {
		t.Fatal("resource lease leaked")
	}
	resources.mu.Unlock()
	guest := newAccountTestBrowser(t, app)
	if result := guest.request(t, http.MethodPost, "/api/ui/admin/playback", defaultPlaybackSettings()); result.Code != 403 {
		t.Fatal("guest changed playback budgets", result.Code)
	}
}

func TestEmbyOriginalMP4SignedRangeWithoutCookies(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "4", "320x180")
	app.mediaResources().settings.Enabled = true
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	task := fixture.tasks[0]
	query := url.Values{"id": {task.DramaID}, "chapter": {task.Chapter.ID}, "key": {embyToken(key, task.DramaID, task.Chapter.ID)}}
	request := httptest.NewRequest("GET", "http://localhost/api/emby/stream.m3u8?"+query.Encode(), nil)
	writer := httptest.NewRecorder()
	app.handleEmbyStream(writer, request)
	if writer.Code != 302 || !strings.Contains(writer.Header().Get("Location"), "file.mp4") {
		t.Fatal("Emby did not receive original MP4", writer.Code, writer.Body.String())
	}
	address := writer.Header().Get("Location")
	part := httptest.NewRequest("GET", "http://localhost"+address, nil)
	part.Header.Set("Range", "bytes=0-15")
	writer = httptest.NewRecorder()
	app.routes().ServeHTTP(writer, part)
	if writer.Code != 206 || writer.Body.Len() != 16 || app.downloader.ffmpegInstaller != nil {
		t.Fatal("Emby original media was not served without FFmpeg", writer.Code, writer.Body.String())
	}
	parsed, _ := url.Parse(address)
	changed := parsed.Query()
	changed.Set("chapter", "forged-episode")
	parsed.RawQuery = changed.Encode()
	writer = httptest.NewRecorder()
	app.routes().ServeHTTP(writer, httptest.NewRequest("GET", "http://localhost"+parsed.String(), nil))
	if writer.Code != 403 {
		t.Fatal("Emby resource accepted a changed signed identity")
	}
	first := address
	writer = httptest.NewRecorder()
	app.handleEmbyStream(writer, request)
	if writer.Header().Get("Location") != first {
		t.Fatal("repeated Emby playback did not share prepared media")
	}
}
