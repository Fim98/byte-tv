package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type providerMedia struct {
	credentials *providerMediaCredentials
	URL         string
	Referer     string
	Duration    time.Duration
	Playlist    string
	HLSKey      []byte
	CENCKey     []byte
	Quality     int
	Variants    []providerMedia
}

func (d *Downloader) providerBaseURL(source string) string {
	if canonicalProviderSource(source) != sourceHongguo {
		return ""
	}
	return strings.TrimRight(firstNonEmpty(d.cfg.HongguoURL, hongguoBaseURL), "/")
}

func providerSourceForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return canonicalProviderSource(parsed.Hostname())
}

func (d *Downloader) providerURLCandidates(raw string) []string {
	source := providerSourceForURL(raw)
	if source == "" {
		return []string{raw}
	}
	parsed, _ := url.Parse(raw)
	d.providerMu.Lock()
	preferred := d.providerHosts[source]
	d.providerMu.Unlock()
	var candidates []string
	seen := map[string]bool{}
	add := func(candidate string) {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	add(rehostProviderURL(parsed, preferred))
	configured := d.providerBaseURL(source)
	add(rehostProviderURL(parsed, configured))
	if providerSourceForURL(configured) == "" {
		return candidates
	}
	add(raw)
	return candidates
}

func (d *Downloader) resolveProviderMedia(ctx context.Context, task Task) (providerMedia, error) {
	if !isHongguoTask(task) {
		return providerMedia{}, fmt.Errorf("此版本仅支持红果剧集")
	}
	chapter := task.Chapter
	if strings.HasPrefix(chapter.VideoURL, "hongguo-cenc://") {
		return d.resolveHongguoMedia(ctx, task)
	}
	media := providerMedia{URL: chapter.VideoURL, Referer: firstNonEmpty(chapter.Referer, d.providerBaseURL(sourceHongguo)+"/")}
	if !isProviderHTTPMediaURL(media.URL) {
		return providerMedia{}, fmt.Errorf("红果未返回有效播放地址，请更新合集或确认站点访问权限")
	}
	parsed, _ := url.Parse(media.URL)
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		selected, err := d.fetchMediaPlaylistForMedia(ctx, media)
		if err != nil {
			return providerMedia{}, fmt.Errorf("获取播放列表失败: %w", err)
		}
		media = selected
	}
	return media, nil
}
