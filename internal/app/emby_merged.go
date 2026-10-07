package app

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

const embyMergedChapterID = "juku:merged"

type embyMergedRecord struct {
	StartEpisode int `json:"startEpisode"`
	EndEpisode   int `json:"endEpisode"`
}

func (record *embyMergedRecord) valid() bool {
	return record != nil && record.StartEpisode >= 1 && record.EndEpisode >= record.StartEpisode && record.EndEpisode <= 2000
}

func (app *UIApp) embyMergedRecord(id string) *embyMergedRecord {
	app.mu.Lock()
	defer app.mu.Unlock()
	state := app.merges[id]
	if state == nil || state.Status != "success" || completedPlaybackPath(&UITask{Status: uiStatusSuccess, Path: state.OutputPath}) == "" {
		return nil
	}
	record := &embyMergedRecord{state.StartEpisode, state.EndEpisode}
	if !record.valid() {
		return nil
	}
	return record
}

func writeEmbyMerged(drama Drama, record *embyMergedRecord, base string, key []byte, owner string, write func(string, []byte) error) error {
	if !record.valid() {
		return fmt.Errorf("合并版集数范围无效")
	}
	episode := struct {
		XMLName xml.Name `xml:"episodedetails"`
		Title   string   `xml:"title"`
		Show    string   `xml:"showtitle"`
		Season  int      `xml:"season"`
		Episode int      `xml:"episode"`
	}{Title: fmt.Sprintf("合并版（第%d–%d集）", record.StartEpisode, record.EndEpisode), Show: drama.DisplayTitle(), Season: 0, Episode: 1}
	body, err := xml.MarshalIndent(episode, "", "  ")
	if err != nil {
		return err
	}
	if err = write("Season 00/S00E001.nfo", append([]byte(xml.Header), body...)); err != nil {
		return err
	}
	query := url.Values{"id": {drama.ID}, "chapter": {embyMergedChapterID}, "key": {embyToken(key, drama.ID, embyMergedChapterID, owner)}}
	if owner != "" {
		query.Set("account", owner)
	}
	return write("Season 00/S00E001.strm", []byte(base+"/api/emby/merged.mp4?"+query.Encode()+"\n"))
}

func (app *UIApp) handleEmbyMerged(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	if chapter != embyMergedChapterID {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "合并版播放链接无效，请重新同步"})
		return
	}
	app.mu.Lock()
	_, path, err := app.mergedPlaybackTaskLocked(mergedPlaybackPrefix + id)
	app.mu.Unlock()
	if err != nil {
		app.writeEmbyPlaybackError(writer, request, http.StatusNotFound, app.redactError(err))
		return
	}
	release, err := app.mediaResources().acquire(request.Context(), "media", false)
	if err != nil {
		app.writeEmbyPlaybackError(writer, request, http.StatusServiceUnavailable, app.redactError(err))
		return
	}
	defer release()
	file, err := os.Open(path)
	if err != nil {
		app.writeEmbyPlaybackError(writer, request, http.StatusNotFound, "本地合并文件无法读取，请检查文件是否移动")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		app.writeEmbyPlaybackError(writer, request, http.StatusNotFound, "本地合并文件无效，请重新合并")
		return
	}
	writer.Header().Set("Content-Type", "video/mp4")
	writer.Header().Set("X-Accel-Buffering", "no")
	http.ServeContent(writer, request, "merged.mp4", info.ModTime(), file)
}

func (app *UIApp) embyExportChapters(ctx context.Context, id string, merged *embyMergedRecord) (string, []Chapter, error) {
	if merged == nil {
		return app.downloader.GetDramaChapters(ctx, id)
	}
	lookup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	title, chapters, err := app.downloader.GetDramaChapters(lookup, id)
	if err != nil && ctx.Err() == nil {
		app.downloader.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "emby.metadata_fallback", DramaID: id, Message: app.redactError(err)})
		return "", nil, nil
	}
	return title, chapters, err
}
