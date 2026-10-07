package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type hlsAsset struct {
	extension string
	remote    string
	body      []byte
	playlist  bool
	fallbacks []hlsAsset
}

type hlsProxy struct {
	downloader  *Downloader
	credentials *providerMediaCredentials
	client      *http.Client
	server      *http.Server
	base        string
	referer     string
	key         []byte
	mediaKey    []byte
	mu          sync.Mutex
	assets      map[string]hlsAsset
	assetIDs    map[string]string
	failure     error
	root        string
	retries     int
	host        string
	diagnostic  func(diagnosticEvent)
	query       string
	acquire     func(context.Context) (func(), error)
	cache       *downloadMediaCache
}

var hlsURIAttribute = regexp.MustCompile(`URI="([^"]+)"`)

func (d *Downloader) newHLSProxy(ctx context.Context, media providerMedia, key []byte) (*hlsProxy, error) {
	nonce := randomHex(16)
	if nonce == "" {
		return nil, errors.New("无法创建本地下载会话")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("创建本地 HLS 转发失败: %w", err)
	}
	proxy := &hlsProxy{
		downloader:  d,
		credentials: media.credentials,
		client:      &http.Client{Transport: d.client.Transport, Jar: d.client.Jar, CheckRedirect: d.client.CheckRedirect},
		base:        "http://" + listener.Addr().String() + "/" + nonce + "/",
		referer:     media.Referer,
		key:         key,
		mediaKey:    media.HLSKey,
		assets:      map[string]hlsAsset{},
		assetIDs:    map[string]string{},
		retries:     d.cfg.Retries,
		diagnostic:  d.recordDiagnostic,
	}
	if remote, err := url.Parse(media.URL); err == nil {
		proxy.host = remote.Hostname()
	}
	proxy.root = proxy.addAsset(playbackRootAsset(media))
	proxy.server = &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		if err := proxy.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			proxy.recordError(err)
		}
	}()
	return proxy, nil
}

func playbackRootAsset(media providerMedia) hlsAsset {
	asset := hlsAsset{remote: media.URL, body: []byte(media.Playlist), playlist: media.Playlist != ""}
	for _, fallback := range mediaFallbackVariants(media) {
		asset.fallbacks = append(asset.fallbacks, hlsAsset{remote: fallback.URL, body: []byte(fallback.Playlist), playlist: fallback.Playlist != ""})
	}
	if asset.playlist && !strings.Contains(media.Playlist, "#EXT-X-ENDLIST") && !strings.Contains(media.Playlist, "#EXT-X-STREAM-INF:") {
		asset.body = nil
	}
	return asset
}

func (proxy *hlsProxy) Close() {
	if proxy.server != nil {
		_ = proxy.server.Close()
	}
}

func (proxy *hlsProxy) Err() error {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return proxy.failure
}

func (proxy *hlsProxy) recordError(err error) {
	if err == nil {
		return
	}
	proxy.mu.Lock()
	first := proxy.failure == nil
	if proxy.failure == nil {
		proxy.failure = publicError(err)
	}
	proxy.mu.Unlock()
	if first && proxy.diagnostic != nil {
		proxy.diagnostic(diagnosticEvent{Event: "media.failed", Host: proxy.host, Message: err.Error()})
	}
}

func (proxy *hlsProxy) addAsset(asset hlsAsset) string {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if existing := proxy.assetIDs[asset.remote]; existing != "" {
		return existing
	}
	if len(proxy.assets) >= 16384 {
		return ""
	}
	extension := asset.extension
	if extension == "" {
		remote, _ := url.Parse(asset.remote)
		if remote != nil {
			extension = strings.ToLower(path.Ext(remote.Path))
		}
		switch extension {
		case ".m3u8":
			asset.playlist = true
		case ".mp4", ".m4s", ".m4a", ".aac", ".mp3", ".vtt", ".key", ".ts":
		default:
			extension = ".ts"
		}
	}
	if asset.playlist {
		extension = ".m3u8"
	} else if len(asset.body) > 0 {
		extension = ".key"
	}
	asset.extension = extension
	local := proxy.base + strconv.Itoa(len(proxy.assets)+1) + extension
	if proxy.query != "" {
		local += "?" + proxy.query
	}
	parsed, _ := url.Parse(local)
	proxy.assets[parsed.Path] = asset
	proxy.assetIDs[asset.remote] = local
	return local
}

func (proxy *hlsProxy) rewritePlaylist(raw, baseURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	rewrite := func(reference string, playlist, key, initialization bool) (string, error) {
		parsed, err := url.Parse(reference)
		if err != nil {
			return "", err
		}
		remote := base.ResolveReference(parsed).String()
		if !isProviderHTTPMediaURL(remote) {
			return "", errors.New("HLS 包含非 HTTP/HTTPS 资源地址")
		}
		asset := hlsAsset{remote: remote, playlist: playlist || strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8")}
		if key {
			asset.extension = ".key"
		} else if initialization {
			asset.extension = ".mp4"
		}
		resolved, _ := url.Parse(remote)
		if key && len(proxy.key) > 0 && strings.Contains(parsed.Path, "/api/app/vid/sec") {
			asset.body = proxy.key
		}
		if key && len(proxy.mediaKey) == 16 && strings.HasSuffix(resolved.Path, "/enc.key") {
			asset.body = proxy.mediaKey
		}
		local := proxy.addAsset(asset)
		if local == "" {
			return "", errors.New("HLS 资源数量超过限制")
		}
		return local, nil
	}
	lines := strings.Split(strings.TrimPrefix(raw, "\ufeff"), "\n")
	nextPlaylist := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#EXT-X-STREAM-INF:") {
			nextPlaylist = true
		}
		if strings.HasPrefix(trimmed, "#") {
			var rewriteErr error
			lines[index] = hlsURIAttribute.ReplaceAllStringFunc(line, func(attribute string) string {
				reference := hlsURIAttribute.FindStringSubmatch(attribute)[1]
				local, err := rewrite(reference, strings.HasPrefix(trimmed, "#EXT-X-MEDIA:") || strings.HasPrefix(trimmed, "#EXT-X-I-FRAME-STREAM-INF:"), strings.HasPrefix(trimmed, "#EXT-X-KEY:") || strings.HasPrefix(trimmed, "#EXT-X-SESSION-KEY:"), strings.HasPrefix(trimmed, "#EXT-X-MAP:"))
				if err != nil {
					rewriteErr = err
					return attribute
				}
				return `URI="` + local + `"`
			})
			if rewriteErr != nil {
				return "", rewriteErr
			}
		} else if trimmed != "" {
			lines[index], err = rewrite(trimmed, nextPlaylist, false, false)
			if err != nil {
				return "", err
			}
			nextPlaylist = false
		}
	}
	return strings.Join(lines, "\n"), nil
}

func hlsAssetContentType(extension string) string {
	switch extension {
	case ".mp4", ".m4s":
		return "video/mp4"
	case ".ts":
		return "video/mp2t"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".mp3":
		return "audio/mpeg"
	case ".vtt":
		return "text/vtt"
	case ".key":
		return "application/octet-stream"
	}
	return ""
}

func (proxy *hlsProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	proxy.mu.Lock()
	asset, found := proxy.assets[request.URL.Path]
	proxy.mu.Unlock()
	if !found {
		http.NotFound(writer, request)
		return
	}
	body := asset.body
	if len(body) == 0 {
		if proxy.acquire != nil {
			release, err := proxy.acquire(request.Context())
			if err != nil {
				http.Error(writer, "媒体连接正在等待资源，请稍后重试", http.StatusServiceUnavailable)
				return
			}
			defer release()
		}
		method := request.Method
		if asset.playlist {
			method = http.MethodGet
		}
		buildRequest := func(candidate hlsAsset) (*http.Request, error) {
			upstream, err := http.NewRequestWithContext(request.Context(), method, candidate.remote, nil)
			if err != nil {
				return nil, err
			}
			upstream.Header.Set("User-Agent", userAgent)
			upstream.Header.Set("Referer", proxy.referer)
			upstream.Header.Set("Accept-Encoding", "identity")
			if origin, err := url.Parse(proxy.referer); err == nil && origin.Host != "" {
				upstream.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
			}
			for _, header := range []string{"Range", "If-Range"} {
				if value := request.Header.Get(header); value != "" && !candidate.playlist {
					upstream.Header.Set(header, value)
				}
			}
			return upstream, nil
		}
		candidates := append([]hlsAsset{asset}, asset.fallbacks...)
		if proxy.cache != nil && !asset.playlist && request.Method == http.MethodGet && request.Header.Get("Range") == "" {
			for _, candidate := range candidates {
				file, size, cacheErr := proxy.cache.open(request.Context(), proxy, candidate, buildRequest)
				if cacheErr == nil {
					defer file.Close()
					if contentType := hlsAssetContentType(candidate.extension); contentType != "" {
						writer.Header().Set("Content-Type", contentType)
					}
					writer.Header().Set("Content-Length", strconv.FormatInt(size, 10))
					http.ServeContent(writer, request, "media", time.Time{}, file)
					return
				}
			}
		}
		var upstream *http.Request
		var response *http.Response
		var err error
		for index, candidate := range candidates {
			upstream, err = buildRequest(candidate)
			if err != nil {
				break
			}
			response, err = proxy.fetch(upstream)
			if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				asset = candidate
				break
			}
			if response != nil {
				if response.StatusCode == http.StatusRequestedRangeNotSatisfiable || response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusGone {
					break
				}
				_ = response.Body.Close()
			}
			if index == len(candidates)-1 {
				break
			}
		}
		if err != nil {
			proxy.fail(writer, request, err)
			return
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
				writer.Header().Set("Content-Range", response.Header.Get("Content-Range"))
				writer.WriteHeader(response.StatusCode)
				return
			}
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
				proxy.recordError(fmt.Errorf("媒体源 HTTP %d", response.StatusCode))
				writer.WriteHeader(response.StatusCode)
				return
			}
			proxy.fail(writer, request, fmt.Errorf("HLS 资源请求失败: %s HTTP %d", upstream.URL.Hostname(), response.StatusCode))
			return
		}
		if asset.playlist || strings.Contains(response.Header.Get("Content-Type"), "mpegurl") {
			defer response.Body.Close()
			body, err = io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
			if err != nil || len(body) > providerMaxBodyBytes {
				proxy.fail(writer, request, errors.New("读取 HLS 播放列表失败或内容过大"))
				return
			}
			asset.playlist = true
			if response.Request != nil && response.Request.URL != nil {
				asset.remote = response.Request.URL.String()
			}
		} else {
			reader := proxy.mediaReader(upstream, response)
			defer reader.Close()
			for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Content-Encoding", "ETag", "Last-Modified"} {
				if value := response.Header.Get(header); value != "" {
					writer.Header().Set(header, value)
				}
			}
			if contentType := hlsAssetContentType(asset.extension); contentType != "" {
				writer.Header().Set("Content-Type", contentType)
			}
			writer.WriteHeader(response.StatusCode)
			if request.Method == http.MethodHead {
				return
			}
			if _, err := io.Copy(writer, reader); err != nil && reader.failure != nil && request.Context().Err() == nil {
				proxy.recordError(reader.failure)
			}
			return
		}
	}
	if asset.playlist {
		if !bytes.HasPrefix(bytes.TrimSpace(bytes.TrimPrefix(body, []byte("\ufeff"))), []byte("#EXTM3U")) {
			proxy.fail(writer, request, errors.New("上游没有返回有效的 HLS 播放列表"))
			return
		}
		rewritten, err := proxy.rewritePlaylist(string(body), asset.remote)
		if err != nil {
			proxy.fail(writer, request, err)
			return
		}
		body = []byte(rewritten)
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else {
		writer.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeContent(writer, request, "media", time.Time{}, bytes.NewReader(body))
}

func (proxy *hlsProxy) fetch(request *http.Request) (*http.Response, error) {
	request = request.Clone(providerMediaContext(request.Context(), proxy.credentials))
	attempts := mediaRequestAttempts(proxy.retries)
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * time.Second)
			select {
			case <-timer.C:
			case <-request.Context().Done():
				timer.Stop()
				return nil, request.Context().Err()
			}
		}
		var response *http.Response
		var err error
		if proxy.downloader != nil {
			response, err = proxy.downloader.doMediaRequestWithClient(request.Clone(request.Context()), proxy.client)
		} else {
			upstream := request.Clone(request.Context())
			mediaRequestHeaders(upstream, proxy.referer)
			client := proxy.client
			if proxy.credentials != nil {
				if err := proxy.credentials.apply(upstream); err != nil {
					return nil, err
				}
				client = proxy.credentials.client(client)
			}
			response, err = client.Do(upstream)
		}
		if err == nil {
			return response, nil
		}
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		lastErr = err
	}
	return nil, fmt.Errorf("媒体资源连接失败（%s，已尝试 %d 次；可在代理设置中检测连接）: %w", request.URL.Hostname(), attempts, publicError(lastErr))
}

func (proxy *hlsProxy) fail(writer http.ResponseWriter, request *http.Request, err error) {
	if request.Context().Err() != nil {
		return
	}
	proxy.recordError(err)
	http.Error(writer, "上游媒体请求失败", http.StatusBadGateway)
}
