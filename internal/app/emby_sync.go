package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type embySyncSettings struct {
	GroupBySource   bool   `json:"-"`
	Enabled         bool   `json:"enabled"`
	OutputDir       string `json:"outputDir"`
	BaseURL         string `json:"baseUrl"`
	IntervalMinutes int    `json:"intervalMinutes"`
	Following       bool   `json:"following"`
	Downloads       bool   `json:"downloads"`
	ServerURL       string `json:"serverUrl"`
	APIKey          string `json:"apiKey,omitempty"`
}

type embySyncEntry struct {
	Merged    bool            `json:"merged,omitempty"`
	Folder    string          `json:"folder,omitempty"`
	Title     string          `json:"title"`
	Episodes  int             `json:"episodes"`
	CheckedAt time.Time       `json:"checkedAt"`
	SyncedAt  time.Time       `json:"syncedAt"`
	Error     string          `json:"error,omitempty"`
	Poster    embyPosterState `json:"poster"`
}

type embySyncDocument struct {
	Version        int                      `json:"version"`
	Settings       embySyncSettings         `json:"settings"`
	OwnerID        string                   `json:"ownerId"`
	OwnerName      string                   `json:"owner"`
	Entries        map[string]embySyncEntry `json:"entries"`
	LastStartedAt  time.Time                `json:"lastStartedAt"`
	LastFinishedAt time.Time                `json:"lastFinishedAt"`
	NextRunAt      time.Time                `json:"nextRunAt"`
	WrittenFiles   int                      `json:"writtenFiles"`
	Succeeded      int                      `json:"succeeded"`
	Failed         int                      `json:"failed"`
	Pending        int                      `json:"pending"`
	Error          string                   `json:"error,omitempty"`
	RefreshPending bool                     `json:"refreshPending"`
	PostersSynced  int                      `json:"postersSynced"`
	PostersPending int                      `json:"postersPending"`
	PostersFailed  int                      `json:"postersFailed"`
	PostersUpdated int                      `json:"postersUpdated"`
	PosterChecked  int                      `json:"-"`
}

type embySyncManager struct {
	app        *UIApp
	path       string
	mu         sync.Mutex
	document   embySyncDocument
	loadErr    error
	running    bool
	generation uint64
	wake       chan struct{}
	cancel     context.CancelFunc
	runCancel  context.CancelFunc
	done       chan struct{}
	closed     bool
	dirty      map[string]bool
}

func (app *UIApp) embySyncer() *embySyncManager {
	app.embySyncOnce.Do(func() {
		manager := &embySyncManager{app: app, path: filepath.Join(app.cfg.dataDirectory(), "emby-sync.json"), wake: make(chan struct{}, 1)}
		manager.document = embySyncDocument{Version: 1, Settings: embySyncSettings{
			OutputDir: filepath.Join(app.cfg.OutputDir, "Emby"), IntervalMinutes: 60, Following: true, Downloads: true,
		}, Entries: make(map[string]embySyncEntry)}
		var stored embySyncDocument
		err := readEmbyJSON(manager.path, &stored, 8<<20)
		if err == nil {
			if stored.Version != 1 || len(stored.Entries) > 10000 {
				err = errors.New("Emby 同步记录格式无效")
			}
			if err == nil {
				err = stored.Settings.normalize()
			}
			if err == nil {
				if stored.Entries == nil {
					stored.Entries = make(map[string]embySyncEntry)
				}
				for id, entry := range stored.Entries {
					canonical, _, valid := playbackHistoryIdentity(id)
					if !valid || canonical != id || !validEmbyFolder(entry.Folder) || entry.Episodes < 0 || entry.Episodes > 2000 {
						err = errors.New("Emby 同步记录含无效的剧集或路径")
						break
					}
				}
			}
			if err == nil {
				manager.document = stored
			}
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			manager.loadErr = err
		}
		app.embySync = manager
	})
	return app.embySync
}

func (settings *embySyncSettings) normalize() error {
	if settings.IntervalMinutes < 5 || settings.IntervalMinutes > 1440 {
		return errors.New("同步间隔范围为 5–1440 分钟")
	}
	if settings.Enabled && !settings.Following && !settings.Downloads {
		return errors.New("请至少选择一种同步范围")
	}
	if len(settings.OutputDir) > 2048 || strings.TrimSpace(settings.OutputDir) == "" || strings.ContainsAny(settings.OutputDir, "\x00\r\n") {
		return errors.New("请输入有效的 Emby 输出目录")
	}
	directory, err := normalizedOutputDirectory(settings.OutputDir)
	if err == nil {
		directory, err = filepath.Abs(directory)
	}
	if err != nil {
		return errors.New("Emby 输出目录无效")
	}
	settings.OutputDir = directory
	if settings.Enabled || settings.BaseURL != "" {
		settings.BaseURL, err = embyBaseURL(settings.BaseURL)
		if err != nil {
			return err
		}
	}
	if settings.ServerURL != "" {
		settings.ServerURL, err = embyBaseURL(settings.ServerURL)
		if err != nil {
			return errors.New("请输入有效的 Emby 服务器 HTTP/HTTPS 地址")
		}
		if settings.Enabled && settings.APIKey == "" {
			return errors.New("填写 Emby 服务器后，需要同时填写 API Key 才能通知扫描和同步海报")
		}
	}
	if len(settings.APIKey) > 2048 || strings.ContainsAny(settings.APIKey, "\x00\r\n") {
		return errors.New("Emby API Key 格式无效")
	}
	return nil
}

func (manager *embySyncManager) start() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.cancel != nil || manager.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager.cancel, manager.done = cancel, make(chan struct{})
	manager.notify()
	go manager.loop(ctx)
}

func (manager *embySyncManager) stop() {
	manager.mu.Lock()
	manager.closed = true
	cancel, done := manager.cancel, manager.done
	manager.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (manager *embySyncManager) notify() {
	select {
	case manager.wake <- struct{}{}:
	default:
	}
}

func (app *UIApp) notifyEmbySync(ids ...string) {
	manager := app.embySyncer()
	manager.mu.Lock()
	enabled := manager.document.Settings.Enabled
	if len(ids) > 0 {
		if manager.dirty == nil {
			manager.dirty = make(map[string]bool)
		}
		for _, id := range ids {
			manager.dirty[id] = true
		}
	}
	manager.mu.Unlock()
	if enabled {
		manager.notify()
	}
}

func (manager *embySyncManager) loop(ctx context.Context) {
	defer close(manager.done)
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-manager.wake:
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		manager.runOnce(ctx)
		manager.mu.Lock()
		delay := time.Until(manager.document.NextRunAt)
		if !manager.document.Settings.Enabled || delay < time.Minute {
			delay = time.Minute
		}
		manager.mu.Unlock()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

func (manager *embySyncManager) owner(id string) (accountRecord, error) {
	store := manager.app.browserViewers().accountStore()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return accountRecord{}, store.err
	}
	for _, account := range store.state.Accounts {
		if account.ID == id && account.Admin && !account.RequirePasswordChange {
			return account, nil
		}
	}
	return accountRecord{}, errors.New("启用同步的管理员账号不可用，请重新登录管理员并保存同步设置")
}

func (manager *embySyncManager) targets(ctx context.Context, config embySyncSettings, owner accountRecord) ([]Drama, error) {
	selected := map[string]Drama{}
	if config.Following {
		viewer := manager.app.browserViewers().acquire(owner.viewerID())
		defer viewer.release()
		entries, err := viewer.followingStore().list()
		if err != nil {
			return nil, err
		}
		history, err := viewer.playbackHistory().list()
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(entries)+len(history))
		for _, entry := range entries {
			ids = append(ids, entry.DramaID)
		}
		for _, entry := range history {
			ids = append(ids, entry.DramaID)
		}
		metadata := manager.app.followingMetadata(ids)
		completed := map[string]bool{}
		for _, entry := range entries {
			completed[entry.DramaID] = entry.Completed && (entry.KnownEpisodes == 0 || metadata[entry.DramaID].KnownEpisodes <= entry.KnownEpisodes)
			if entry.Saved && !completed[entry.DramaID] {
				selected[entry.DramaID] = Drama{ID: entry.DramaID, Title: entry.Title, Source: entry.Source}
			}
		}
		for _, entry := range history {
			if !completed[entry.DramaID] && !(entry.Completed && entry.Index >= entry.Total && entry.Index >= metadata[entry.DramaID].KnownEpisodes) {
				selected[entry.DramaID] = Drama{ID: entry.DramaID, Title: entry.Title, Source: entry.Source}
			}
		}
	}
	app := manager.app
	app.mu.Lock()
	if config.Downloads {
		for _, task := range app.tasks {
			if task != nil && task.Status == uiStatusSuccess && !isChapterPlaceholderTask(task) && taskSourceAllowed(ctx, task.Source) {
				selected[task.DramaID] = Drama{ID: task.DramaID, Title: task.DramaTitle, Source: task.Source.Chapter.Source}
			}
		}
		for id, merged := range app.merges {
			if merged != nil && merged.Status == "success" && dramaAllowed(ctx, id, sourceFromDramaID(id)) && completedPlaybackPath(&UITask{Status: uiStatusSuccess, Path: merged.OutputPath}) != "" {
				selected[id] = Drama{ID: id, Title: merged.DramaTitle, Source: sourceFromDramaID(id)}
			}
		}
	}
	for _, drama := range app.dramas {
		if _, exists := selected[drama.ID]; exists {
			selected[drama.ID] = drama
		}
	}
	app.mu.Unlock()
	if len(selected) > 1000 {
		return nil, errors.New("自动同步最多支持 1000 部，请缩小追剧或下载同步范围")
	}
	result := make([]Drama, 0, len(selected))
	for _, drama := range selected {
		id, _, valid := playbackHistoryIdentity(drama.ID)
		if valid && id == drama.ID && dramaAllowed(ctx, drama.ID, drama.Source) {
			result = append(result, drama)
		}
	}
	return result, nil
}

func (manager *embySyncManager) runOnce(parent context.Context) {
	manager.mu.Lock()
	if manager.running || manager.loadErr != nil || !manager.document.Settings.Enabled || manager.closed {
		manager.mu.Unlock()
		return
	}
	previous := manager.document
	document := manager.document
	document.Entries = make(map[string]embySyncEntry, len(manager.document.Entries))
	for id, entry := range manager.document.Entries {
		document.Entries[id] = entry
	}
	dirty := manager.dirty
	manager.dirty = nil
	for id := range dirty {
		if entry, exists := document.Entries[id]; exists {
			entry.CheckedAt = time.Time{}
			document.Entries[id] = entry
		}
	}
	generation := manager.generation
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	manager.running, manager.runCancel = true, cancel
	manager.document.LastStartedAt = time.Now()
	document.LastStartedAt = manager.document.LastStartedAt
	manager.mu.Unlock()
	defer cancel()
	document.WrittenFiles, document.Succeeded, document.Failed, document.Pending, document.Error = 0, 0, 0, 0, ""
	document.PostersUpdated, document.PosterChecked = 0, 0
	document.NextRunAt = time.Now().Add(time.Duration(document.Settings.IntervalMinutes) * time.Minute)
	err := manager.synchronize(ctx, &document)
	if err != nil {
		document.Error = manager.safeError(err, document.Settings.APIKey)
	}
	document.LastFinishedAt = time.Now()
	if document.Pending > 0 || document.RefreshPending || time.Until(document.NextRunAt) < time.Minute {
		document.NextRunAt = time.Now().Add(time.Minute)
	}
	if document.Succeeded == 0 && document.Failed == 0 && document.PosterChecked == 0 && err == nil && !previous.RefreshPending {
		document.LastStartedAt, document.LastFinishedAt = previous.LastStartedAt, previous.LastFinishedAt
		document.Succeeded, document.Failed, document.WrittenFiles, document.Error = previous.Succeeded, previous.Failed, previous.WrittenFiles, previous.Error
	}
	manager.mu.Lock()
	manager.running, manager.runCancel = false, nil
	if generation == manager.generation {
		if err = writeEmbyJSON(manager.path, document, 0600); err != nil {
			document.Error = "同步状态保存失败：" + manager.safeError(err, document.Settings.APIKey)
		}
		manager.document = document
	} else {
		if manager.dirty == nil {
			manager.dirty = make(map[string]bool)
		}
		for id := range dirty {
			manager.dirty[id] = true
		}
	}
	if len(manager.dirty) > 0 {
		manager.notify()
	}
	manager.mu.Unlock()
}

func (manager *embySyncManager) safeError(err error, key string) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if key != "" {
		text = strings.ReplaceAll(text, key, "[redacted]")
	}
	return truncate(manager.app.redactError(errors.New(text)), 1024)
}

func (manager *embySyncManager) synchronize(ctx context.Context, document *embySyncDocument) error {
	manager.app.mu.Lock()
	document.Settings.GroupBySource = manager.app.cfg.GroupBySource
	manager.app.mu.Unlock()
	owner, err := manager.owner(document.OwnerID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(withSourceScope(ctx, owner), backgroundCatalogKey{}, true)
	targets, err := manager.targets(ctx, document.Settings, owner)
	if err != nil {
		return err
	}
	interval := time.Duration(document.Settings.IntervalMinutes) * time.Minute
	defer func() {
		next := time.Now().Add(interval)
		for _, drama := range targets {
			if due := document.Entries[drama.ID].CheckedAt.Add(interval); due.Before(next) {
				next = due
			}
			if due := embyPosterDue(document.Entries[drama.ID].Poster, interval); !due.IsZero() && due.Before(next) {
				next = due
			}
		}
		document.NextRunAt = next
		document.countPosters(targets)
	}()
	var key []byte
	if len(targets) > 0 {
		key, err = manager.app.embySigningKey(true)
		if err != nil {
			return err
		}
	}
	due := make([]Drama, 0, len(targets))
	for _, drama := range targets {
		if time.Since(document.Entries[drama.ID].CheckedAt) >= interval {
			due = append(due, drama)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		left, right := document.Entries[due[i].ID].CheckedAt, document.Entries[due[j].ID].CheckedAt
		if left.Equal(right) {
			return due[i].ID < due[j].ID
		}
		return left.Before(right)
	})
	document.Pending = len(due)
	if len(due) > 50 {
		due = due[:50]
	}
	if len(due) > 0 {
		for _, drama := range due {
			if err := ctx.Err(); err != nil {
				return err
			}
			entry := document.Entries[drama.ID]
			entry.Title, entry.CheckedAt = drama.DisplayTitle(), time.Now()
			dramaCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			merged := manager.app.embyMergedRecord(drama.ID)
			title, chapters, fetchErr := manager.app.embyExportChapters(dramaCtx, drama.ID, merged)
			if fetchErr == nil {
				drama.Title = firstNonEmpty(title, drama.DisplayTitle())
				var written, episodes int
				entry.Folder, written, episodes, fetchErr = syncEmbyDramaFiles(dramaCtx, document.Settings, drama, chapters, entry.Folder, key, owner.ID, merged)
				document.WrittenFiles += written
				if written > 0 {
					document.RefreshPending = document.Settings.ServerURL != ""
				}
				if fetchErr == nil {
					entry.Title, entry.Episodes, entry.SyncedAt = drama.DisplayTitle(), episodes, time.Now()
					entry.Merged = entry.Merged || merged != nil
				}
			}
			cancel()
			entry.Error = manager.safeError(fetchErr, document.Settings.APIKey)
			if fetchErr == nil {
				document.Succeeded++
			} else {
				document.Failed++
			}
			document.Entries[drama.ID] = entry
			document.Pending--
		}
	}
	if document.WrittenFiles > 0 {
		document.RefreshPending = document.Settings.ServerURL != ""
	}
	manager.preparePosters(document, targets, key)
	manager.synchronizeLocalPosters(ctx, document, targets)
	if document.RefreshPending && document.Settings.ServerURL != "" {
		if err := notifyEmbyLibrary(ctx, document.Settings); err != nil {
			return fmt.Errorf("文件已保留，通知 Emby 扫描失败：%w", err)
		}
		document.RefreshPending = false
	}
	manager.synchronizePosters(ctx, document, targets, key)
	document.countPosters(targets)
	if document.Failed > 0 {
		return fmt.Errorf("%d 部同步失败，已导入内容保留，可查看下方详情或稍后重试", document.Failed)
	}
	return document.posterError()
}

func notifyEmbyLibrary(ctx context.Context, settings embySyncSettings) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.ServerURL+"/Library/Refresh", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-Emby-Token", settings.APIKey)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return publicError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Emby HTTP %d", response.StatusCode)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return err
}

func readEmbyJSON(path string, target any, limit int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("Emby 配置或清单格式无效")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > limit {
		return errors.New("Emby 配置或清单过大")
	}
	return nil
}

func writeEmbyJSON(path string, value any, mode os.FileMode) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	_, err = writeEmbyFile(path, append(body, '\n'), mode)
	return err
}
