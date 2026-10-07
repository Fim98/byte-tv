package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

const rankingCacheVersion = 1
const rankingPersistentMaxAge = 24 * time.Hour

type rankingCacheDocument struct {
	Version int           `json:"version"`
	Pages   []rankingPage `json:"pages"`
}

func rankingCachePath(directory string) string {
	return filepath.Join(directory, "rankings.json")
}

func validStoredRankingPage(page rankingPage, now time.Time) bool {
	if page.BoardID == "" || page.Page < 1 || page.Page > 500 || page.FetchedAt.IsZero() || now.Sub(page.FetchedAt) < 0 || now.Sub(page.FetchedAt) > rankingPersistentMaxAge || len(page.Items) > 100 {
		return false
	}
	board, found := findRankingBoard(page.BoardID)
	if !found {
		return false
	}
	seen, previous := map[string]bool{}, 0
	for _, item := range page.Items {
		if item.Rank <= 0 || item.Drama.ID == "" || item.Drama.Source == "" || !matchesSourceFilter(item.Drama.Source, board.Source) || seen[item.Drama.ID] {
			return false
		}
		seen[item.Drama.ID] = true
		if item.Rank < previous {
			return false
		}
		previous = item.Rank
	}
	return true
}

func readRankingCache(directory string) (map[string]rankingPage, error) {
	body, err := os.ReadFile(rankingCachePath(directory))
	if err != nil {
		return nil, err
	}
	var document rankingCacheDocument
	if err := json.Unmarshal(body, &document); err != nil || document.Version != rankingCacheVersion || len(document.Pages) > 128 {
		return nil, os.ErrInvalid
	}
	now := time.Now()
	pages := map[string]rankingPage{}
	for _, page := range document.Pages {
		if !validStoredRankingPage(page, now) {
			return nil, os.ErrInvalid
		}
		clean := cloneRankingPage(page)
		clean.Stale = false
		clean.Warning = ""
		pages[clean.BoardID+":"+strconv.Itoa(clean.Page)] = clean
	}
	return pages, nil
}

func writeRankingCache(directory string, pages map[string]rankingPage) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(pages))
	for key := range pages {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := pages[keys[i]], pages[keys[j]]
		if left.FetchedAt.Equal(right.FetchedAt) {
			return keys[i] < keys[j]
		}
		return left.FetchedAt.After(right.FetchedAt)
	})
	document := rankingCacheDocument{Version: rankingCacheVersion}
	now := time.Now()
	for _, key := range keys {
		page := cloneRankingPage(pages[key])
		page.Stale = false
		page.Warning = ""
		if validStoredRankingPage(page, now) {
			document.Pages = append(document.Pages, page)
		}
		if len(document.Pages) == 128 {
			break
		}
	}
	file, err := os.CreateTemp(directory, ".rankings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(document); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), rankingCachePath(directory))
}

func (d *Downloader) restoreRankingCacheLocked(cache *rankingCache) {
	if cache.pending == nil {
		cache.pending = map[string]*rankingCall{}
	}
	if cache.pages == nil {
		cache.pages = map[string]rankingPage{}
	}
	if cache.loaded {
		return
	}
	cache.loaded = true
	pages, err := readRankingCache(d.cfg.dataDirectory())
	if err != nil {
		return
	}
	for key, page := range pages {
		cache.pages[key] = page
	}
}

func (d *Downloader) saveRankingCacheLocked(cache *rankingCache) {
	if err := writeRankingCache(d.cfg.dataDirectory(), cache.pages); err != nil && d.diagnostics != nil {
		d.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "ranking.cache", Message: "榜单缓存保存失败: " + err.Error()})
	}
}
