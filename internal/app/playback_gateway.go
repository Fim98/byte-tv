package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type playbackMediaPlan struct {
	URL        string                  `json:"url"`
	Run        uint64                  `json:"run"`
	Delivery   string                  `json:"delivery"`
	Processing string                  `json:"processing"`
	Player     string                  `json:"player"`
	MIME       string                  `json:"mime"`
	Reason     string                  `json:"reason"`
	Source     string                  `json:"source"`
	Duration   float64                 `json:"duration"`
	Quality    int                     `json:"quality"`
	Qualities  []playbackQualityOption `json:"qualities"`
	DirectURL  string                  `json:"directURL,omitempty"`
	SeekEnd    float64                 `json:"seekEnd"`
}

type playbackMediaSession struct {
	ctx       context.Context
	cancel    context.CancelFunc
	media     providerMedia
	key       []byte
	local     string
	proxy     *hlsProxy
	plan      playbackMediaPlan
	info      playbackMediaInfo
	generated *playbackGenerated
	bytes     atomic.Int64
	failed    atomic.Bool
	started   time.Time
	closeOnce sync.Once
}

func (media *playbackMediaSession) Close() {
	media.closeOnce.Do(func() {
		media.cancel()
		if media.generated != nil {
			media.generated.Close()
		}
		if media.proxy != nil {
			media.proxy.Close()
		}
	})
}

func (app *UIApp) resolveMediaSession(ctx, preparation context.Context, task Task, downloadID string, quality int, prefetched ...*playbackPrefetch) (*playbackMediaSession, error) {
	ctx, cancel := context.WithCancel(context.WithValue(ctx, playbackQualityKey{}, quality))
	result := &playbackMediaSession{ctx: ctx, cancel: cancel, started: time.Now()}
	success := false
	defer func() {
		if !success {
			result.Close()
		}
	}()
	if downloadID != "" {
		var err error
		task, result.local, err = app.playbackCollectionTask(downloadID)
		if err != nil {
			return nil, err
		}
	}
	usedPrefetch := false
	if result.local == "" && len(prefetched) > 0 && prefetched[0] != nil && prefetched[0].planOnly {
		cache := prefetched[0]
		select {
		case <-cache.done:
		case <-preparation.Done():
			return nil, preparation.Err()
		}
		cache.mu.Lock()
		if cache.resolved != nil && cache.err == nil && cache.resolved.local == "" {
			result.media, result.key, usedPrefetch = cache.resolved.media, cache.resolved.key, true
		}
		cache.mu.Unlock()
		if usedPrefetch && !validProviderMediaCredentials(result.media.credentials, providerMediaCredentialReserve) {
			usedPrefetch = false
		}
	}
	if result.local == "" && !usedPrefetch {
		resolution := context.WithValue(preparation, playbackQualityKey{}, quality)
		var err error
		result.media, result.key, err = app.downloader.resolvePlaybackMedia(resolution, task)
		if err != nil {
			return nil, err
		}
		result.media, err = app.downloader.selectPlaybackQuality(resolution, result.media)
		if err != nil {
			return nil, err
		}
		if len(result.media.CENCKey) != 0 && (len(result.media.CENCKey) != 16 || result.media.Playlist != "") {
			return nil, errors.New("播放密钥或媒体格式无效")
		}
	}
	result.plan = playbackMediaPlan{Delivery: "proxy", Processing: "original", Player: "mp4", Source: "online",
		MIME: "video/mp4", Reason: "compatible_original", Duration: result.media.Duration.Seconds(),
		Quality: result.media.Quality, Qualities: playbackQualityOptions(result.media)}
	if result.local != "" {
		result.plan.Delivery, result.plan.Source = "local", "local"
	}
	if result.media.Playlist != "" {
		result.plan.Player, result.plan.MIME = "hls", "application/vnd.apple.mpegurl"
	}
	result.plan.SeekEnd = result.plan.Duration
	success = true
	return result, nil
}

func (app *UIApp) attachMediaGateway(media *playbackMediaSession, base, query string) {
	proxy := &hlsProxy{downloader: app.downloader, credentials: media.media.credentials, client: &http.Client{Transport: app.downloader.client.Transport, Jar: app.downloader.client.Jar, CheckRedirect: app.downloader.client.CheckRedirect}, base: base, referer: media.media.Referer,
		key: media.key, mediaKey: media.media.HLSKey, assets: map[string]hlsAsset{}, assetIDs: map[string]string{},
		retries: app.downloader.cfg.Retries, diagnostic: app.downloader.recordDiagnostic, query: query,
		acquire: func(ctx context.Context) (func(), error) { return app.mediaResources().acquire(ctx, "media", false) }}
	if remote, err := url.Parse(media.media.URL); err == nil {
		proxy.host = remote.Hostname()
	}
	proxy.root = proxy.addAsset(playbackRootAsset(media.media))
	if media.plan.Player == "mp4" {
		proxy.root = base + "file.mp4"
		if query != "" {
			proxy.root += "?" + query
		}
		proxy.assets = map[string]hlsAsset{base + "file.mp4": {remote: media.media.URL}}
		proxy.assetIDs = map[string]string{media.media.URL: proxy.root}
	}
	media.proxy = proxy
	media.plan.URL = proxy.root
	if media.local != "" {
		media.plan.URL = base + "file.mp4"
		if query != "" {
			media.plan.URL += "?" + query
		}
	}
}

type playbackCountingWriter struct {
	http.ResponseWriter
	media    *playbackMediaSession
	activity func()
}

func (writer *playbackCountingWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *playbackCountingWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound || status == http.StatusGone || status == http.StatusTooManyRequests || status >= 500 {
		writer.media.failed.Store(true)
	}
	writer.ResponseWriter.WriteHeader(status)
}
func (writer *playbackCountingWriter) Write(body []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(body)
	writer.media.bytes.Add(int64(n))
	if n > 0 && writer.activity != nil {
		writer.activity()
	}
	return n, err
}

func (app *UIApp) serveMediaSession(writer http.ResponseWriter, request *http.Request, media *playbackMediaSession) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if media.ctx.Err() != nil {
		writer.WriteHeader(http.StatusGone)
		return
	}
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	idle := time.AfterFunc(60*time.Second, cancel)
	defer idle.Stop()
	stop := context.AfterFunc(media.ctx, cancel)
	defer stop()
	request = request.WithContext(ctx)
	writer.Header().Set("Cache-Control", "private, no-store, no-transform")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(writer)
	interrupted := make(chan struct{})
	interrupt := context.AfterFunc(ctx, func() { _ = controller.SetWriteDeadline(time.Now()); close(interrupted) })
	defer func() {
		if !interrupt() {
			<-interrupted
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	writer = &playbackCountingWriter{ResponseWriter: writer, media: media, activity: func() { idle.Reset(60 * time.Second) }}
	if media.plan.Delivery == "redirect" {
		if request.URL.Path != strings.Split(media.plan.URL, "?")[0] {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Location", media.media.URL)
		writer.WriteHeader(http.StatusFound)
		return
	}
	if media.generated != nil {
		media.generated.ServeHTTP(writer, request)
		return
	}
	if media.local != "" {
		if request.URL.Path != strings.Split(media.plan.URL, "?")[0] {
			http.NotFound(writer, request)
			return
		}
		file, err := os.Open(media.local)
		if err != nil {
			http.NotFound(writer, request)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(writer, request, "file.mp4", info.ModTime(), file)
		return
	}
	if len(request.Header.Get("Range")) > 256 || strings.Contains(request.Header.Get("Range"), ",") {
		writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	media.proxy.ServeHTTP(writer, request)
}

func (app *UIApp) handlePlaybackMediaAsset(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, request.Method) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/api/ui/playback/media/"), "/")
	if len(parts) != 3 {
		http.NotFound(writer, request)
		return
	}
	run, err := strconv.ParseUint(parts[1], 10, 64)
	app.playbackMu.Lock()
	session := app.playbacks[parts[0]]
	if err != nil || !viewerOwnsPlayback(request.Context(), session) || session.run != run || session.media == nil {
		app.playbackMu.Unlock()
		writer.WriteHeader(http.StatusGone)
		return
	}
	media := session.media
	active := &playbackRunContext{id: session.id, run: session.run, session: session, ctx: media.ctx}
	app.touchPlaybackLocked(session)
	app.playbackMu.Unlock()
	writer = &playbackActivityWriter{ResponseWriter: writer, app: app, run: active}
	app.serveMediaSession(writer, request, media)
}
