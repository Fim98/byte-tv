package app

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var reDateText = regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}`)
var reResolution = regexp.MustCompile(`(?i)RESOLUTION\s*=\s*(\d+)x(\d+)`)
var reBandwidth = regexp.MustCompile(`(?i)(?:AVERAGE-)?BANDWIDTH\s*=\s*(\d+)`)

func providerDramaID(source, sourceID string) string {
	return source + ":" + strings.TrimSpace(sourceID)
}

func providerChapterID(source, sourceID, chapterKey string) string {
	return source + ":" + strings.TrimSpace(sourceID) + ":" + strings.TrimSpace(chapterKey)
}

func rehostProviderURL(u *url.URL, mirror string) string {
	mu, err := url.Parse(mirror)
	if err != nil || mu.Host == "" {
		return ""
	}
	copyURL := *u
	copyURL.Scheme = mu.Scheme
	copyURL.Host = mu.Host
	return copyURL.String()
}

func providerRefererForURL(candidate, fallback string) string {
	if u, err := url.Parse(candidate); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/"
	}
	return fallback
}

func rawEpisode(n int) json.RawMessage {
	if n <= 0 {
		n = 1
	}
	return json.RawMessage(strconv.Itoa(n))
}

func selectBestM3U8Variant(master, masterURL string) string {
	playlistURL, err := url.Parse(masterURL)
	if err != nil {
		return ""
	}
	lines := strings.Split(master, "\n")
	type variant struct {
		url       string
		height    int
		bandwidth int
		order     int
	}
	var variants []variant
	pending := variant{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#EXT-X-STREAM-INF") {
			pending = variant{}
			if m := reResolution.FindStringSubmatch(trimmed); len(m) > 2 {
				pending.height, _ = strconv.Atoi(m[2])
			}
			if m := reBandwidth.FindStringSubmatch(trimmed); len(m) > 1 {
				pending.bandwidth, _ = strconv.Atoi(m[1])
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if pending.height > 0 || pending.bandwidth > 0 {
			reference, err := url.Parse(trimmed)
			if err != nil {
				pending = variant{}
				continue
			}
			pending.url = playlistURL.ResolveReference(reference).String()
			if !isProviderHTTPMediaURL(pending.url) {
				pending = variant{}
				continue
			}
			pending.order = len(variants)
			variants = append(variants, pending)
			pending = variant{}
		}
	}
	if len(variants) == 0 {
		return ""
	}
	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].height != variants[j].height {
			return variants[i].height > variants[j].height
		}
		if variants[i].bandwidth != variants[j].bandwidth {
			return variants[i].bandwidth > variants[j].bandwidth
		}
		return variants[i].order < variants[j].order
	})
	return variants[0].url
}

func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	matches := reDateText.FindAllString(s, -1)
	for _, m := range matches {
		parts := strings.Split(m, "-")
		if len(parts) != 3 {
			continue
		}
		y, yErr := strconv.Atoi(parts[0])
		mo, moErr := strconv.Atoi(parts[1])
		d, dErr := strconv.Atoi(parts[2])
		if yErr == nil && moErr == nil && dErr == nil && y >= 2000 && y <= 2100 && mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return fmt.Sprintf("%04d-%02d-%02d", y, mo, d)
		}
	}
	return ""
}

func normalizeViews(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "次播放") {
		return s
	}
	if strings.ContainsAny(strings.ToLower(s), "w万") {
		return s + "次播放"
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 {
		return s
	}
	return strconv.FormatFloat(n, 'f', -1, 64) + "次播放"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func mapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch x := v.(type) {
			case string:
				if text := strings.TrimSpace(x); text != "" {
					return text
				}
			case bool:
				return strconv.FormatBool(x)
			case float64:
				if x == float64(int64(x)) {
					return strconv.FormatInt(int64(x), 10)
				}
				return strconv.FormatFloat(x, 'f', -1, 64)
			case int:
				return strconv.Itoa(x)
			case json.Number:
				return x.String()
			}
		}
	}
	return ""
}

func mapStringSlice(m map[string]any, keys ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, key := range keys {
		v, ok := m[key]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case string:
			for _, part := range strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == '/' || r == '，' || r == '、' }) {
				add(part)
			}
		case []any:
			for _, item := range x {
				add(fmt.Sprint(item))
			}
		}
	}
	return out
}

func isProviderHTTPMediaURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" && parsed.User == nil
}

func extractAttr(block string, names ...string) string {
	for _, name := range names {
		re := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*["']([^"']+)["']`)
		if m := re.FindStringSubmatch(block); len(m) > 1 {
			return strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	return ""
}
