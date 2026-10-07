package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type playbackSettings struct {
	Enabled            bool     `json:"enabled"`
	DirectSources      []string `json:"directSources"`
	MaxSessions        int      `json:"maxSessions"`
	MaxMediaRequests   int      `json:"maxMediaRequests"`
	MaxRemuxJobs       int      `json:"maxRemuxJobs"`
	MaxAudioTranscodes int      `json:"maxAudioTranscodes"`
	MaxVideoTranscodes int      `json:"maxVideoTranscodes"`
	MaxPrefetchJobs    int      `json:"maxPrefetchJobs"`
	CacheMB            int      `json:"cacheMB"`
}

type playbackResources struct {
	mu         sync.Mutex
	saveMu     sync.Mutex
	settings   playbackSettings
	active     map[string]int
	waiting    map[string]int
	changed    chan struct{}
	path       string
	cacheBytes int64
	background map[string]map[*playbackPrefetch]int
}

func defaultPlaybackSettings() playbackSettings {
	return playbackSettings{Enabled: false, DirectSources: []string{}, MaxSessions: 4, MaxMediaRequests: 8,
		MaxRemuxJobs: 2, MaxAudioTranscodes: 1, MaxVideoTranscodes: 1, MaxPrefetchJobs: 1, CacheMB: 512}
}

func (settings playbackSettings) validate() error {
	if settings.MaxSessions < 1 || settings.MaxSessions > 64 || settings.MaxMediaRequests < 1 || settings.MaxMediaRequests > 64 ||
		settings.MaxRemuxJobs < 0 || settings.MaxRemuxJobs > 8 || settings.MaxAudioTranscodes < 0 || settings.MaxAudioTranscodes > 8 ||
		settings.MaxVideoTranscodes < 0 || settings.MaxVideoTranscodes > 8 || settings.MaxPrefetchJobs < 0 || settings.MaxPrefetchJobs > 4 ||
		settings.CacheMB < 64 || settings.CacheMB > 8192 {
		return errors.New("播放资源设置超出允许范围")
	}
	seen := make(map[string]bool)
	for _, source := range settings.DirectSources {
		valid := false
		for _, choice := range accountSourceChoices {
			valid = valid || source == choice.ID
		}
		if !valid || seen[source] {
			return errors.New("直连站源选项无效")
		}
		seen[source] = true
	}
	return nil
}

func (app *UIApp) mediaResources() *playbackResources {
	app.playbackResourcesOnce.Do(func() {
		resources := &playbackResources{settings: defaultPlaybackSettings(), active: make(map[string]int), waiting: make(map[string]int),
			changed: make(chan struct{}), path: filepath.Join(app.cfg.dataDirectory(), "playback-settings.json")}
		if body, err := os.ReadFile(resources.path); err == nil {
			settings := defaultPlaybackSettings()
			if len(body) <= 16384 && json.Unmarshal(body, &settings) == nil && settings.validate() == nil {
				resources.settings = settings
			}
		}
		app.playbackResources = resources
	})
	return app.playbackResources
}

func (resources *playbackResources) snapshot() playbackSettings {
	resources.mu.Lock()
	defer resources.mu.Unlock()
	settings := resources.settings
	settings.DirectSources = append([]string{}, settings.DirectSources...)
	return settings
}

func (settings playbackSettings) limit(kind string) int {
	switch kind {
	case "media":
		return settings.MaxMediaRequests
	case "remux":
		return settings.MaxRemuxJobs
	case "audio":
		return settings.MaxAudioTranscodes
	case "video":
		return settings.MaxVideoTranscodes
	case "prefetch":
		return settings.MaxPrefetchJobs
	}
	return 0
}

func (resources *playbackResources) notifyLocked() {
	close(resources.changed)
	resources.changed = make(chan struct{})
}

func (resources *playbackResources) acquire(ctx context.Context, kind string, optional bool) (func(), error) {
	prefetch, _ := ctx.Value(playbackPrefetchKey{}).(*playbackPrefetch)
	resources.mu.Lock()
	if resources.waiting[kind] >= 128 {
		resources.mu.Unlock()
		return nil, errors.New("播放资源等待队列已满，请稍后重试")
	}
	resources.waiting[kind]++
	defer func() {
		resources.mu.Lock()
		resources.waiting[kind]--
		resources.notifyLocked()
		resources.mu.Unlock()
	}()
	for {
		if prefetch != nil && prefetch.foreground.Load() {
			optional = false
		}
		limit := resources.settings.limit(kind)
		if err := ctx.Err(); err != nil {
			resources.mu.Unlock()
			return nil, err
		}
		if limit == 0 {
			resources.mu.Unlock()
			return nil, fmt.Errorf("管理员已禁用此类播放处理（%s）", kind)
		}
		if resources.active[kind] < limit && (!optional || resources.waiting[kind] == 1) {
			resources.active[kind]++
			tracked := optional && prefetch != nil
			if tracked {
				if resources.background == nil {
					resources.background = make(map[string]map[*playbackPrefetch]int)
				}
				if resources.background[kind] == nil {
					resources.background[kind] = make(map[*playbackPrefetch]int)
				}
				resources.background[kind][prefetch]++
			}
			resources.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					resources.mu.Lock()
					resources.active[kind]--
					if tracked {
						resources.background[kind][prefetch]--
						if resources.background[kind][prefetch] == 0 {
							delete(resources.background[kind], prefetch)
						}
					}
					resources.notifyLocked()
					resources.mu.Unlock()
				})
			}, nil
		}
		if optional {
			resources.mu.Unlock()
			return nil, errors.New("前台播放正在使用资源，已跳过本次预缓存")
		}
		var interrupt []*playbackPrefetch
		for cache := range resources.background[kind] {
			if !cache.foreground.Load() {
				interrupt = append(interrupt, cache)
			}
		}
		changed := resources.changed
		resources.mu.Unlock()
		for _, cache := range interrupt {
			cache.cancelBackground()
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resources.mu.Lock()
	}
}

func (app *UIApp) handlePlaybackSettings(writer http.ResponseWriter, request *http.Request) {
	resources := app.mediaResources()
	if request.Method == http.MethodGet {
		writeJSON(writer, http.StatusOK, map[string]any{"settings": resources.snapshot(), "sources": accountSourceChoices})
		return
	}
	var settings playbackSettings
	if !readPlaybackRequest(writer, request, &settings) {
		return
	}
	if err := settings.validate(); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	resources.saveMu.Lock()
	defer resources.saveMu.Unlock()
	body, _ := json.MarshalIndent(settings, "", "  ")
	err := os.MkdirAll(filepath.Dir(resources.path), 0700)
	if err == nil {
		var file *os.File
		file, err = os.CreateTemp(filepath.Dir(resources.path), ".playback-settings-*")
		if err == nil {
			defer os.Remove(file.Name())
			_, err = file.Write(body)
			if syncErr := file.Sync(); err == nil {
				err = syncErr
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(file.Name(), resources.path)
			}
		}
	}
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "播放设置保存失败"})
		return
	}
	resources.mu.Lock()
	resources.settings = settings
	resources.notifyLocked()
	resources.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"settings": settings, "sources": accountSourceChoices})
}
