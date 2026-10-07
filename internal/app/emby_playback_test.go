package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func embyScanFixture(t *testing.T, optimized bool) (*UIApp, func(int) string) {
	t.Helper()
	app, fixture := nativePlaybackFixture(t, "4", "160x90")
	app.closePlayback(fixture.id)
	app.mediaResources().settings.Enabled = optimized
	app.mediaResources().settings.MaxSessions = 1
	for index := 1; index <= 8; index++ {
		task := fixture.tasks[0]
		task.Index, task.Total = index, 8
		task.Chapter.ID = fmt.Sprintf("%s:%d", task.DramaID, index)
		task.Chapter.CurrentEpisode = rawEpisode(index)
		id := fmt.Sprintf("scan-%d", index)
		app.tasks[id] = &UITask{ID: id, DramaID: task.DramaID, Status: uiStatusSuccess, Path: task.OutPath, Source: task}
		app.taskOrder = append(app.taskOrder, id)
	}
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	return app, func(index int) string {
		task := app.tasks[fmt.Sprintf("scan-%d", index)].Source
		query := url.Values{"id": {task.DramaID}, "chapter": {task.Chapter.ID}, "key": {embyToken(key, task.DramaID, task.Chapter.ID)}}
		return "/api/emby/stream.m3u8?" + query.Encode()
	}
}

func embyRequest(app *UIApp, method, address string) *httptest.ResponseRecorder {
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, httptest.NewRequest(method, "http://localhost"+address, nil))
	return writer
}

func embyFirstSegment(t *testing.T, playlist string) string {
	t.Helper()
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "segment.ts?") {
			return "/api/emby/" + line
		}
	}
	t.Fatal("missing signed HLS segment")
	return ""
}

func TestEmbySerialProbesReleaseSlotsAndReusePlayback(t *testing.T) {
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	for _, optimized := range []bool{false, true} {
		t.Run(fmt.Sprintf("original=%t", optimized), func(t *testing.T) {
			app, address := embyScanFixture(t, optimized)
			server := httptest.NewServer(app.routes())
			defer server.Close()
			first := embyRequest(app, http.MethodGet, address(1))
			firstBody, firstLocation := first.Body.String(), first.Header().Get("Location")
			for index := 1; index <= 8; index++ {
				if head := embyRequest(app, http.MethodHead, address(index)); head.Code != 200 || head.Body.Len() != 0 {
					t.Fatal("HEAD failed", head.Code)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				output, err := exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", server.URL+address(index)).CombinedOutput()
				cancel()
				if err != nil || !strings.Contains(string(output), "4.000000") {
					t.Fatalf("serial probe %d failed: %v %s", index, err, output)
				}
				app.playbackMu.Lock()
				active := app.activePlaybackSessionsLocked()
				cached := 0
				for _, session := range app.playbacks {
					if session.native != nil {
						cached++
					}
				}
				app.playbackMu.Unlock()
				if active != 0 || cached > 1 {
					t.Fatal("finished probe retained a slot or unbounded media cache", active, cached)
				}
			}
			if !optimized {
				part := embyRequest(app, http.MethodGet, embyFirstSegment(t, firstBody))
				if part.Code != 200 || part.Body.Len() < 188 || part.Body.Bytes()[0] != 0x47 {
					t.Fatal("evicting an idle cache broke an existing HLS URL", part.Code, part.Body.String())
				}
			}
			repeated := embyRequest(app, http.MethodGet, address(1))
			if repeated.Code != first.Code || repeated.Body.String() != firstBody || repeated.Header().Get("Location") != firstLocation {
				t.Fatal("repeated episode did not reuse its prepared session", repeated.Code)
			}
			web := historyJSONRequest(t, app, app.handlePlaybackOpen, "/api/ui/playback/open", map[string]string{"taskId": "scan-1"})
			if web.Code != 200 {
				t.Fatal("completed Emby scans blocked a new browser playback", web.Code, web.Body.String())
			}
		})
	}
}

type embyBlockingWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (writer *embyBlockingWriter) Write(body []byte) (int, error) {
	writer.once.Do(func() {
		close(writer.entered)
		<-writer.resume
	})
	return writer.ResponseRecorder.Write(body)
}

func TestEmbyActiveRequestsShareLimitAndReleaseOnCompletion(t *testing.T) {
	for _, optimized := range []bool{false, true} {
		t.Run(fmt.Sprintf("original=%t", optimized), func(t *testing.T) {
			app, address := embyScanFixture(t, optimized)
			first := embyRequest(app, http.MethodGet, address(1))
			asset := first.Header().Get("Location")
			if !optimized {
				asset = embyFirstSegment(t, first.Body.String())
			}
			writer := &embyBlockingWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), resume: make(chan struct{})}
			done := make(chan struct{})
			go func() {
				defer close(done)
				app.routes().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "http://localhost"+asset, nil))
			}()
			var resume sync.Once
			defer resume.Do(func() { close(writer.resume); <-done })
			select {
			case <-writer.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("media request never reached the response writer")
			}
			if second := embyRequest(app, http.MethodGet, address(2)); second.Code != 429 || second.Header().Get("Retry-After") == "" {
				t.Fatal("active requests bypassed the configured limit", second.Code)
			}
			if same := embyRequest(app, http.MethodGet, address(1)); same.Code != first.Code {
				t.Fatal("concurrent requests for the same episode were counted twice", same.Code)
			}
			web := historyJSONRequest(t, app, app.handlePlaybackOpen, "/api/ui/playback/open", map[string]string{"taskId": "scan-1"})
			if web.Code != 429 {
				t.Fatal("browser playback bypassed an active Emby request", web.Code)
			}
			resume.Do(func() { close(writer.resume); <-done })
			if writer.Code != 200 {
				t.Fatal("the original media request was interrupted", writer.Code)
			}
			if second := embyRequest(app, http.MethodGet, address(2)); second.Code != first.Code {
				t.Fatal("completed request did not release its slot", second.Code)
			}
			web = historyJSONRequest(t, app, app.handlePlaybackOpen, "/api/ui/playback/open", map[string]string{"taskId": "scan-1"})
			if web.Code != 200 {
				t.Fatal("released Emby slot was not available to the browser", web.Code, web.Body.String())
			}
			var opened struct{ Session string }
			if err := json.Unmarshal(web.Body.Bytes(), &opened); err != nil {
				t.Fatal(err)
			}
			app.closePlayback(opened.Session)
		})
	}
}

func TestEmbyCanceledProbeReleasesPendingPreparation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	var startOnce, stopOnce sync.Once
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		startOnce.Do(func() { close(started) })
		<-request.Context().Done()
		stopOnce.Do(func() { close(stopped) })
		return nil, request.Context().Err()
	})
	app := &UIApp{downloader: d, cfg: d.cfg}
	t.Cleanup(app.closePlaybacks)
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	id, chapter := "hongguo:7000000000000000001", "hongguo:7000000000000000001:1"
	query := url.Values{"id": {id}, "chapter": {chapter}, "key": {embyToken(key, id, chapter)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		request := httptest.NewRequest(http.MethodGet, "http://localhost/api/emby/stream.m3u8?"+query.Encode(), nil).WithContext(ctx)
		app.handleEmbyStream(httptest.NewRecorder(), request)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not start")
	}
	other, cancelOther := context.WithCancel(context.Background())
	defer cancelOther()
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		request := httptest.NewRequest(http.MethodGet, "http://localhost/api/emby/stream.m3u8?"+query.Encode(), nil).WithContext(other)
		app.handleEmbyStream(httptest.NewRecorder(), request)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		app.playbackMu.Lock()
		waiters := 0
		for _, session := range app.playbacks {
			waiters += session.mediaWaiters
		}
		app.playbackMu.Unlock()
		if waiters == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second request did not join the preparation")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first canceled request did not finish")
	}
	select {
	case <-stopped:
		t.Fatal("one canceled client stopped another client's preparation")
	default:
	}
	cancelOther()
	for _, signal := range []chan struct{}{otherDone, stopped} {
		select {
		case <-signal:
		case <-time.After(5 * time.Second):
			t.Fatal("canceling the probe left its preparation running")
		}
	}
	app.playbackMu.Lock()
	defer app.playbackMu.Unlock()
	if len(app.playbacks) != 0 {
		t.Fatal("canceled probe retained a playback session")
	}
}

func TestEmbyEvictedRemuxRestoresExistingSegmentURLs(t *testing.T) {
	app, address := embyScanFixture(t, true)
	first := embyRequest(app, http.MethodGet, address(1))
	if first.Code != http.StatusFound {
		t.Fatal(first.Code, first.Body.String())
	}
	parsed, _ := url.Parse(first.Header().Get("Location"))
	base := strings.TrimSuffix(parsed.Path, "file.mp4")
	sessionID := strings.Split(base, "/")[4]
	app.playbackMu.Lock()
	session := app.playbacks[sessionID]
	media := session.media
	app.playbackMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.generatePlayback(ctx, media, "remux", base, parsed.RawQuery); err != nil {
		t.Fatal(err)
	}
	segment := media.generated.assetURL(media.generated.segments[0].Name)
	before := embyRequest(app, http.MethodGet, segment)
	if before.Code != 200 {
		t.Fatal("prepared remux was unreadable", before.Code)
	}
	if next := embyRequest(app, http.MethodGet, address(2)); next.Code != http.StatusFound {
		t.Fatal("could not prepare another episode", next.Code)
	}
	if media.ctx.Err() == nil {
		t.Fatal("idle remux cache was not released")
	}
	after := embyRequest(app, http.MethodGet, segment)
	if after.Code != 200 || !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("reclaiming cached files changed or invalidated an existing segment", after.Code)
	}
}

func TestEmbyPlaybackCacheRemainsAccountScoped(t *testing.T) {
	for _, optimized := range []bool{false, true} {
		t.Run(fmt.Sprintf("original=%t", optimized), func(t *testing.T) {
			app, address := embyScanFixture(t, optimized)
			store := app.browserViewers().accountStore()
			store.mu.Lock()
			for _, owner := range []string{"owner-a", "owner-b"} {
				store.state.Accounts[owner] = accountRecord{ID: owner, Sources: []string{sourceHongguo}}
			}
			store.mu.Unlock()
			key, err := app.embySigningKey(false)
			if err != nil {
				t.Fatal(err)
			}
			sign := func(path, owner string) string {
				parsed, _ := url.Parse(path)
				query := parsed.Query()
				query.Set("account", owner)
				query.Set("key", embyToken(key, query.Get("id"), query.Get("chapter"), owner))
				parsed.RawQuery = query.Encode()
				return parsed.String()
			}
			first := embyRequest(app, http.MethodGet, sign(address(1), "owner-a"))
			asset := first.Header().Get("Location")
			if !optimized {
				asset = embyFirstSegment(t, first.Body.String())
			}
			if forged := embyRequest(app, http.MethodGet, sign(asset, "owner-b")); forged.Code != http.StatusGone {
				t.Fatal("valid credentials for another account reused a private cache", forged.Code)
			}
			second := embyRequest(app, http.MethodGet, sign(address(1), "owner-b"))
			if second.Code != first.Code {
				t.Fatal("second account could not prepare its own session", second.Code)
			}
			app.playbackMu.Lock()
			count := len(app.playbacks)
			app.playbackMu.Unlock()
			if count != 2 {
				t.Fatal("accounts shared a playback session", count)
			}
		})
	}
}
