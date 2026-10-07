package app

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// 客户端新接口：/api/desktop/episodes、/api/desktop/home、/api/desktop/status。
// 全部只读，复用现有剧库、下载任务、观看记录与追剧数据，不改变旧接口语义。

type desktopDramaDetail struct {
	desktopDramaCard
	Intro         string   `json:"intro,omitempty"`
	Heat          string   `json:"heat,omitempty"`
	Views         string   `json:"views,omitempty"`
	Score         string   `json:"score,omitempty"`
	OnlineDate    string   `json:"onlineDate,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ReleaseStatus string   `json:"releaseStatus,omitempty"`
	VIP           bool     `json:"vip,omitempty"`
}

type desktopEpisodeView struct {
	Index     int     `json:"index"`
	Number    int     `json:"number"`
	Episode   string  `json:"episode"`
	Title     string  `json:"title"`
	ChapterID string  `json:"chapterId,omitempty"`
	VIP       bool    `json:"vip,omitempty"`
	Local     bool    `json:"local"`
	TaskID    string  `json:"taskId,omitempty"`
	Watched   bool    `json:"watched"`
	Current   bool    `json:"current"`
	Position  float64 `json:"position,omitempty"`
	Duration  float64 `json:"duration,omitempty"`
}

type desktopChapterCacheEntry struct {
	title    string
	chapters []Chapter
	fetched  time.Time
}

var desktopChapterCache = struct {
	sync.Mutex
	entries map[string]desktopChapterCacheEntry
}{entries: map[string]desktopChapterCacheEntry{}}

const desktopChapterCacheTTL = 5 * time.Minute

func (a *UIApp) desktopChapters(ctx context.Context, dramaID string, refresh bool) (string, []Chapter, error) {
	desktopChapterCache.Lock()
	cached, ok := desktopChapterCache.entries[dramaID]
	desktopChapterCache.Unlock()
	if ok && !refresh && time.Since(cached.fetched) < desktopChapterCacheTTL {
		return cached.title, cached.chapters, nil
	}
	title, chapters, err := a.downloader.GetDramaChapters(ctx, dramaID)
	if err != nil {
		return "", nil, err
	}
	desktopChapterCache.Lock()
	if len(desktopChapterCache.entries) > 256 {
		desktopChapterCache.entries = map[string]desktopChapterCacheEntry{}
	}
	desktopChapterCache.entries[dramaID] = desktopChapterCacheEntry{title: title, chapters: chapters, fetched: time.Now()}
	desktopChapterCache.Unlock()
	return title, chapters, nil
}

func desktopDetail(card desktopDramaCard, drama Drama) desktopDramaDetail {
	detail := desktopDramaDetail{desktopDramaCard: card, Intro: firstNonEmpty(drama.Intro, drama.Desc), Heat: drama.Heat, Views: drama.Views, Score: drama.Score, OnlineDate: drama.OnlineDate, Tags: drama.Tags, ReleaseStatus: dramaReleaseStatus(drama)}
	if drama.VIP != nil {
		detail.VIP = *drama.VIP
	}
	return detail
}

func (a *UIApp) findDramaLocked(id string) (Drama, bool) {
	for _, candidate := range a.dramas {
		if candidate.ID == id {
			return candidate, true
		}
	}
	return Drama{}, false
}

// handleDesktopEpisodes 合并在线分集、本地下载状态与观看进度。
// GET /api/desktop/episodes?id=hongguo:123[&refresh=1]
func (a *UIApp) handleDesktopEpisodes(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	id, source, valid := playbackHistoryIdentity(strings.TrimSpace(request.URL.Query().Get("id")))
	if !valid {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "剧集 ID 无效"})
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	if !dramaAllowed(request.Context(), id, source) {
		writeSourceDenied(writer)
		return
	}
	history, hasHistory := viewer.playbackHistory().get(id)
	a.mu.Lock()
	drama, found := a.findDramaLocked(id)
	local := a.desktopLocalEpisodesLocked(id)
	a.mu.Unlock()
	if !found {
		drama = Drama{ID: id, Source: source}
		if hasHistory {
			drama.Title = history.Title
		}
	}
	localByNumber := map[int]desktopEpisode{}
	for _, episode := range local {
		number := episodeNumberFromString(episode.Episode, episode.Index)
		localByNumber[number] = episode
	}
	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()
	title, chapters, chapterErr := a.desktopChapters(ctx, id, request.URL.Query().Get("refresh") == "1")
	if title != "" && drama.Title == "" {
		drama.Title = title
	}
	episodes := make([]desktopEpisodeView, 0, len(chapters))
	for index, chapter := range chapters {
		number := index + 1
		view := desktopEpisodeView{Index: index + 1, Number: number, Episode: chapter.EpisodeString(number), Title: strings.TrimSpace(chapter.Title), ChapterID: chapter.ID, VIP: chapter.VIP}
		if localEpisode, ok := localByNumber[episodeNumberFromString(view.Episode, number)]; ok {
			view.Local, view.TaskID = true, localEpisode.TaskID
		}
		episodes = append(episodes, view)
	}
	onlineError := ""
	if chapterErr != nil || len(chapters) == 0 {
		if chapterErr != nil {
			onlineError = "在线选集获取失败：" + a.redactError(chapterErr)
		} else {
			onlineError = "站点未返回可播放的分集"
		}
		episodes = episodes[:0]
		for _, episode := range local {
			episodes = append(episodes, desktopEpisodeView{Index: episode.Index, Number: episodeNumberFromString(episode.Episode, episode.Index), Episode: episode.Episode, Title: episode.Title, Local: true, TaskID: episode.TaskID})
		}
	}
	var progress map[string]any
	if hasHistory {
		for index := range episodes {
			if episodes[index].Index < history.Index || episodes[index].Index == history.Index && history.Completed {
				episodes[index].Watched = true
			}
			if episodes[index].Index == history.Index {
				episodes[index].Current = true
				episodes[index].Position, episodes[index].Duration = history.Position, history.Duration
			}
		}
		progress = map[string]any{"index": history.Index, "episode": history.Episode, "position": history.Position, "duration": history.Duration, "completed": history.Completed, "mode": history.Mode, "watchedAt": history.WatchedAt}
	}
	var following *followingEntry
	if entries, err := viewer.followingStore().list(); err == nil {
		for _, entry := range entries {
			if entry.DramaID == id {
				copied := entry
				following = &copied
				break
			}
		}
	}
	a.mu.Lock()
	card := a.desktopCardLocked(drama, len(local) > 0)
	a.mu.Unlock()
	if card.TotalEpisode < len(episodes) && onlineError == "" {
		card.TotalEpisode = len(episodes)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"drama": desktopDetail(card, drama), "inLibrary": found, "episodes": episodes, "total": len(episodes), "localCount": len(local), "progress": progress, "following": following, "onlineError": onlineError})
}

func episodeNumberFromString(value string, fallback int) int {
	number := 0
	for _, r := range strings.TrimSpace(value) {
		if r < '0' || r > '9' {
			break
		}
		number = number*10 + int(r-'0')
		if number > 100000 {
			break
		}
	}
	if number < 1 {
		return fallback
	}
	return number
}

// handleDesktopHome 首页聚合：继续观看、追剧（含新集数）、本地已下载、剧库精选与状态。
// 只读本地数据，不访问网络；榜单与分类推荐仍由前端调用 /api/ui/rankings、/api/ui/recommendations。
func (a *UIApp) handleDesktopHome(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	history, historyErr := viewer.playbackHistory().list()
	sort.SliceStable(history, func(i, j int) bool { return history[i].WatchedAt.After(history[j].WatchedAt) })
	following, followingErr := viewer.followingStore().list()
	ids := make([]string, 0, len(following))
	for _, entry := range following {
		ids = append(ids, entry.DramaID)
	}
	known := a.followingMetadata(ids)

	type continueItem struct {
		desktopDramaCard
		Episode   string    `json:"episode"`
		Index     int       `json:"index"`
		Total     int       `json:"total"`
		Position  float64   `json:"position"`
		Duration  float64   `json:"duration"`
		Completed bool      `json:"completed"`
		Mode      string    `json:"mode"`
		WatchedAt time.Time `json:"watchedAt"`
	}
	type followingItem struct {
		desktopDramaCard
		Saved         bool      `json:"saved"`
		Completed     bool      `json:"completed"`
		KnownEpisodes int       `json:"knownEpisodes"`
		AddedAt       time.Time `json:"addedAt"`
		NewEpisodes   int       `json:"newEpisodes"`
	}
	ctx := request.Context()
	a.mu.Lock()
	localIDs := map[string]bool{}
	for _, id := range a.taskOrder {
		task := a.tasks[id]
		if task != nil && completedPlaybackPath(task) != "" && isPlayableDownloadTask(task) {
			localIDs[task.DramaID] = true
		}
	}
	byID := make(map[string]Drama, len(a.dramas))
	for _, drama := range a.dramas {
		byID[drama.ID] = drama
	}
	cardFor := func(id, source, title string) desktopDramaCard {
		drama, ok := byID[id]
		if !ok {
			drama = Drama{ID: id, Source: source, Title: title}
		}
		return a.desktopCardLocked(drama, localIDs[id])
	}
	continueWatching := make([]continueItem, 0)
	for _, entry := range history {
		if len(continueWatching) >= 20 {
			break
		}
		if entry.Completed && entry.Index >= entry.Total || !dramaAllowed(ctx, entry.DramaID, entry.Source) {
			continue
		}
		continueWatching = append(continueWatching, continueItem{desktopDramaCard: cardFor(entry.DramaID, entry.Source, entry.Title), Episode: entry.Episode, Index: entry.Index, Total: entry.Total, Position: entry.Position, Duration: entry.Duration, Completed: entry.Completed, Mode: entry.Mode, WatchedAt: entry.WatchedAt})
	}
	followingItems := make([]followingItem, 0, len(following))
	newCount := 0
	for _, entry := range following {
		if !dramaAllowed(ctx, entry.DramaID, entry.Source) {
			continue
		}
		item := followingItem{desktopDramaCard: cardFor(entry.DramaID, entry.Source, entry.Title), Saved: entry.Saved, Completed: entry.Completed, KnownEpisodes: entry.KnownEpisodes, AddedAt: entry.AddedAt}
		if current, ok := known[entry.DramaID]; ok && entry.KnownEpisodes > 0 && current.KnownEpisodes > entry.KnownEpisodes {
			item.NewEpisodes = current.KnownEpisodes - entry.KnownEpisodes
			newCount++
		}
		followingItems = append(followingItems, item)
	}
	sort.SliceStable(followingItems, func(i, j int) bool { return followingItems[i].NewEpisodes > followingItems[j].NewEpisodes })
	localCards := make([]desktopDramaCard, 0)
	featured := make([]desktopDramaDetail, 0, 24)
	for _, drama := range a.dramas {
		if !dramaAllowed(ctx, drama.ID, drama.Source) {
			continue
		}
		if localIDs[drama.ID] && len(localCards) < 40 {
			localCards = append(localCards, a.desktopCardLocked(drama, true))
		}
		if len(featured) < 24 {
			featured = append(featured, desktopDetail(a.desktopCardLocked(drama, localIDs[drama.ID]), drama))
		}
	}
	libraryTotal, loadedAt, loading := len(a.dramas), a.loadedAt, a.libraryLoading != nil
	a.mu.Unlock()
	warnings := []string{}
	if historyErr != nil {
		warnings = append(warnings, "观看记录读取失败："+publicError(historyErr).Error())
	}
	if followingErr != nil {
		warnings = append(warnings, "追剧清单读取失败："+publicError(followingErr).Error())
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"continue": continueWatching, "following": followingItems, "followingNew": newCount,
		"local": localCards, "featured": featured,
		"library":  map[string]any{"total": libraryTotal, "loadedAt": loadedAt, "loading": loading},
		"warnings": warnings,
	})
}

// handleDesktopStatus 客户端状态：数据目录、下载目录、FFmpeg、剧库数量、进行中任务数（退出确认用）。
func (a *UIApp) handleDesktopStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	a.mu.Lock()
	cfg := a.cfg
	active := 0
	for _, id := range a.taskOrder {
		task := a.tasks[id]
		if task != nil && (task.Status == uiStatusRunning || task.Status == uiStatusQueued || task.Status == uiStatusParsing) {
			active++
		}
	}
	libraryTotal, loadedAt := len(a.dramas), a.loadedAt
	client := a.clientMode
	merging := len(a.mergeJobs)
	a.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{
		"clientMode": client, "owner": isClientOwner(request.Context()),
		"dataDir": cfg.dataDirectory(), "outputDir": cfg.OutputDir,
		"ffmpeg":          a.downloader.ffmpegInstallation().snapshot(),
		"library":         map[string]any{"total": libraryTotal, "loadedAt": loadedAt},
		"activeDownloads": active, "activeMerges": merging,
	})
}
