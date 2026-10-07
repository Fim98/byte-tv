package app

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (app *UIApp) allowPlaybackDirect(ctx context.Context, media *playbackMediaSession, source, origin string) bool {
	allowed := false
	for _, selected := range app.mediaResources().snapshot().DirectSources {
		allowed = allowed || selected == accountSourceGroup(source)
	}
	if !allowed || media.local != "" || media.media.credentials != nil || len(media.key) != 0 || len(media.media.HLSKey) != 0 || len(media.media.CENCKey) != 0 {
		return false
	}
	parsed, err := url.Parse(media.media.URL)
	page, pageErr := url.Parse(origin)
	if err != nil || pageErr != nil || page.Host == "" || parsed.User != nil || !isProviderHTTPMediaURL(parsed.String()) || page.Scheme == "https" && parsed.Scheme != "https" {
		return false
	}
	if media.plan.Player == "mp4" && (!media.info.MP4 || !media.info.Range) {
		return false
	}
	if media.plan.Player == "hls" && len(media.media.Variants) > 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := &http.Client{Transport: app.downloader.client.Transport, CheckRedirect: func(request *http.Request, previous []*http.Request) error {
		if len(previous) >= 5 || request.URL.User != nil || page.Scheme == "https" && request.URL.Scheme != "https" {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	seen := make(map[string]bool)
	manifests := 0
	var check func(string, bool) bool
	check = func(address string, playlist bool) bool {
		if seen[address] {
			return true
		}
		if len(seen) >= 256 || ctx.Err() != nil {
			return false
		}
		seen[address] = true
		remote, err := url.Parse(address)
		if err != nil || remote.User != nil || !isProviderHTTPMediaURL(address) || page.Scheme == "https" && remote.Scheme != "https" {
			return false
		}
		release, err := app.mediaResources().acquire(ctx, "media", false)
		if err != nil {
			return false
		}
		method := http.MethodHead
		if playlist || media.plan.Player == "mp4" {
			method = http.MethodGet
		}
		request, err := http.NewRequestWithContext(ctx, method, address, nil)
		if err != nil {
			release()
			return false
		}
		request.Header.Set("Origin", origin)
		request.Header.Set("Accept-Encoding", "identity")
		if !playlist && method == http.MethodGet {
			request.Header.Set("Range", "bytes=0-0")
		}
		response, err := client.Do(request)
		if err != nil {
			release()
			return false
		}
		valid := response.StatusCode >= 200 && response.StatusCode < 300
		if media.plan.Player == "mp4" {
			valid = response.StatusCode == http.StatusPartialContent && strings.HasPrefix(response.Header.Get("Content-Range"), "bytes 0-0/")
		}
		if media.plan.Player == "hls" {
			cors := response.Header.Get("Access-Control-Allow-Origin")
			valid = valid && (cors == "*" || cors == origin)
		}
		var body []byte
		if valid && playlist {
			manifests++
			body, err = io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
			valid = err == nil && len(body) <= 256*1024 && manifests <= 8 && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff")), "#EXTM3U")
		}
		response.Body.Close()
		release()
		if !valid || !playlist {
			return valid
		}
		base := response.Request.URL
		nextPlaylist := false
		resolve := func(reference string, childPlaylist bool) bool {
			child, err := url.Parse(reference)
			return err == nil && check(base.ResolveReference(child).String(), childPlaylist)
		}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
				nextPlaylist = true
				continue
			}
			if !strings.HasPrefix(line, "#") {
				if !resolve(line, nextPlaylist) {
					return false
				}
				nextPlaylist = false
				continue
			}
			for _, tag := range []string{"#EXT-X-MEDIA:", "#EXT-X-I-FRAME-STREAM-INF:", "#EXT-X-KEY:", "#EXT-X-SESSION-KEY:", "#EXT-X-MAP:"} {
				if !strings.HasPrefix(line, tag) {
					continue
				}
				for _, match := range hlsURIAttribute.FindAllStringSubmatch(line, -1) {
					if !resolve(match[1], tag == "#EXT-X-MEDIA:" || tag == "#EXT-X-I-FRAME-STREAM-INF:") {
						return false
					}
				}
			}
		}
		return true
	}
	return check(media.media.URL, media.plan.Player == "hls")
}
