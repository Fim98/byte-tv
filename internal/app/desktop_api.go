package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type desktopDramaCard struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Cover        string `json:"cover,omitempty"`
	Category     string `json:"category,omitempty"`
	TotalEpisode int    `json:"totalEpisode,omitempty"`
	Local        bool   `json:"local"`
}

type desktopEpisode struct {
	Index   int    `json:"index"`
	Episode string `json:"episode"`
	Title   string `json:"title"`
	TaskID  string `json:"taskId,omitempty"`
	Local   bool   `json:"local"`
}

func desktopCover(drama Drama) string {
	cover, _ := drama.Cover.(string)
	return strings.TrimSpace(cover)
}

func desktopTitleMatch(drama Drama, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(drama.DisplayTitle()), query)
}

func (a *UIApp) desktopLocalEpisodesLocked(dramaID string) []desktopEpisode {
	var tasks []*UITask
	for _, id := range a.taskOrder {
		task := a.tasks[id]
		if task == nil || task.DramaID != dramaID || completedPlaybackPath(task) == "" || !isPlayableDownloadTask(task) {
			continue
		}
		tasks = append(tasks, task)
	}
	sort.SliceStable(tasks, func(left, right int) bool {
		return playbackEpisodeNumber(tasks[left]) < playbackEpisodeNumber(tasks[right])
	})
	episodes := make([]desktopEpisode, 0, len(tasks))
	for index, task := range tasks {
		number := playbackEpisodeNumber(task)
		if number < 1 {
			number = index + 1
		}
		episode := strings.TrimSpace(task.Episode)
		if episode == "" {
			episode = task.Source.Chapter.EpisodeString(number)
		}
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = strings.TrimSpace(task.Source.Chapter.Title)
		}
		if title == "" {
			title = "第" + episode + "集"
		}
		episodes = append(episodes, desktopEpisode{Index: index + 1, Episode: episode, Title: title, TaskID: task.ID, Local: true})
	}
	return episodes
}

func (a *UIApp) desktopCardLocked(drama Drama, local bool) desktopDramaCard {
	return desktopDramaCard{
		ID:           drama.ID,
		Title:        drama.DisplayTitle(),
		Cover:        desktopCover(drama),
		Category:     firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.Category, drama.TypeName, drama.SortName),
		TotalEpisode: followingEpisodeCount(drama),
		Local:        local,
	}
}

func (a *UIApp) handleDesktopCatalog(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	query := request.URL.Query().Get("q")
	a.mu.Lock()
	dramas := append([]Drama(nil), a.dramas...)
	localIDs := map[string]bool{}
	for _, id := range a.taskOrder {
		task := a.tasks[id]
		if task != nil && completedPlaybackPath(task) != "" && isPlayableDownloadTask(task) {
			localIDs[task.DramaID] = true
		}
	}
	cards := make([]desktopDramaCard, 0)
	for _, drama := range dramas {
		if !desktopTitleMatch(drama, query) || !dramaAllowed(request.Context(), drama.ID, drama.Source) {
			continue
		}
		cards = append(cards, a.desktopCardLocked(drama, localIDs[drama.ID]))
	}
	a.mu.Unlock()
	if strings.TrimSpace(query) == "" {
		line := fmt.Sprintf("桌面剧库目录: %d 部", len(cards))
		if len(cards) > 0 {
			line += " 首部: " + cards[0].Title
		}
		fmt.Println(line)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": cards, "total": len(cards)})
}

func (a *UIApp) handleDesktopDrama(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	id := strings.TrimSpace(request.URL.Query().Get("id"))
	if _, _, valid := playbackHistoryIdentity(id); !valid {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "剧集 ID 无效"})
		return
	}
	a.mu.Lock()
	var drama Drama
	for _, candidate := range a.dramas {
		if candidate.ID == id {
			drama = candidate
			break
		}
	}
	episodes := a.desktopLocalEpisodesLocked(id)
	card := a.desktopCardLocked(drama, len(episodes) > 0)
	a.mu.Unlock()
	if drama.ID == "" || !dramaAllowed(request.Context(), drama.ID, drama.Source) {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此短剧不在当前剧库中"})
		return
	}
	if episodes == nil {
		episodes = []desktopEpisode{}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"drama": card, "episodes": episodes})
}

func (a *UIApp) Handler() http.Handler { return a.routes() }

// WindowHandler serves the existing 果果剧库 page and its API inside the desktop window.
func (a *UIApp) WindowHandler() http.Handler {
	next := a.routes()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			a.attachDesktopViewer(writer, request)
		}
		next.ServeHTTP(writer, request)
	})
}

func desktopViewerIdentity(directory string) string {
	path := filepath.Join(directory, "desktop-viewer")
	if body, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(body))
		if len(value) == 64 {
			return value
		}
	}
	identity := randomHex(32)
	if identity == "" {
		return ""
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ""
	}
	if err := writeNewViewerFile(path, []byte(identity)); err != nil {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return ""
		}
		return strings.TrimSpace(string(body))
	}
	return identity
}

func (a *UIApp) attachDesktopViewer(writer http.ResponseWriter, request *http.Request) {
	manager := a.browserViewers()
	if manager.err != nil || manager.identity(request) != "" {
		return
	}
	identity := desktopViewerIdentity(manager.directory)
	if identity == "" {
		return
	}
	expires := time.Now().Add(viewerLifetime)
	request.AddCookie(&http.Cookie{Name: manager.cookie, Value: manager.token(identity, expires)})
	manager.setGuestCookie(writer, request, identity)
}

func (a *UIApp) enableWindowPlayback() {
	resources := a.mediaResources()
	resources.mu.Lock()
	resources.settings.Enabled = true
	resources.mu.Unlock()
}

func (a *UIApp) PrepareDesktop() error {
	a.enableWindowPlayback()
	if err := a.prepareBrowserViewers(); err != nil {
		return err
	}
	a.embySyncer().start()
	installer := a.downloader.ffmpegInstallation()
	go func() {
		path, err := installer.ensure(context.Background())
		if err != nil {
			fmt.Printf("FFmpeg 准备失败：%v\n", publicError(err))
			return
		}
		fmt.Printf("FFmpeg 已就绪：%s\n", path)
	}()
	return nil
}

func (a *UIApp) CloseDesktop() {
	if a.embySync != nil {
		a.embySyncer().stop()
	}
	if a.downloader != nil && a.downloader.ffmpegInstaller != nil {
		a.downloader.ffmpegInstallation().stop()
	}
	a.stopSortMetadata()
	a.closePlaybacks()
	a.stopMergeJobs()
	a.mu.Lock()
	a.closed = true
	if a.cond != nil {
		a.cond.Broadcast()
	}
	a.mu.Unlock()
}
