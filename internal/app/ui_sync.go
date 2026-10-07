package app

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"
)

const maxViewerSyncBytes = 12 << 20

const viewerSyncPackageVersion = 1

type viewerSyncPackage struct {
	Version    int                    `json:"version"`
	ExportedAt time.Time              `json:"exportedAt"`
	History    []playbackHistoryEntry `json:"history"`
	Following  []followingEntry       `json:"following"`
}

func (app *UIApp) handleViewerSyncPackage(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		app.handleViewerSyncPackageExport(writer, request)
	case http.MethodPost:
		app.handleViewerSyncPackageImport(writer, request)
	default:
		writer.Header().Set("Allow", "GET, POST")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
	}
}

func (app *UIApp) handleViewerSyncPackageExport(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	history, err := viewer.playbackHistory().list()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法读取观看记录：" + publicError(err).Error()})
		return
	}
	following, err := viewer.followingStore().list()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法读取追剧清单：" + publicError(err).Error()})
		return
	}
	pack := viewerSyncPackage{Version: viewerSyncPackageVersion, ExportedAt: time.Now().UTC()}
	for _, entry := range history {
		if dramaAllowed(request.Context(), entry.DramaID, entry.Source) {
			pack.History = append(pack.History, entry)
		}
	}
	for _, entry := range following {
		if dramaAllowed(request.Context(), entry.DramaID, entry.Source) {
			pack.Following = append(pack.Following, entry)
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"package": pack, "limits": map[string]int{"history": playbackHistoryLimit, "following": followingLimit}})
}

func (app *UIApp) handleViewerSyncPackageImport(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodPost) {
		return
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "同步包必须使用 JSON"})
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	var pack viewerSyncPackage
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxViewerSyncBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pack); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "同步包格式无效：" + err.Error()})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "同步包包含多余数据"})
		return
	}
	if pack.Version != viewerSyncPackageVersion {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "同步包版本不兼容"})
		return
	}
	if len(pack.History) > playbackHistoryLimit*4 || len(pack.Following) > followingLimit*4 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "同步包记录过多"})
		return
	}
	history, historyRejected := viewerSyncHistoryEntries(request.Context(), pack.History)
	following, followingRejected := viewerSyncFollowingEntries(request.Context(), pack.Following)
	historyImported, err := viewer.playbackHistory().merge(history)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "观看记录未能保存：" + publicError(err).Error()})
		return
	}
	followingImported, followingSkipped, err := viewer.followingStore().merge(following)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "追剧清单未能保存：" + publicError(err).Error()})
		return
	}
	if historyImported > 0 || followingImported > 0 {
		app.notifyEmbySync()
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "historyImported": historyImported, "historyRejected": historyRejected, "followingImported": followingImported, "followingRejected": followingRejected, "followingSkipped": followingSkipped})
}

func viewerSyncHistoryEntries(ctx context.Context, entries []playbackHistoryEntry) ([]playbackHistoryEntry, int) {
	result := make([]playbackHistoryEntry, 0, len(entries))
	rejected := 0
	for _, entry := range entries {
		entry, valid := normalizePlaybackHistoryEntry(entry)
		if !valid || !dramaAllowed(ctx, entry.DramaID, entry.Source) {
			rejected++
			continue
		}
		entry.sessionID = ""
		entry.sessionOpened = time.Time{}
		result = append(result, entry)
	}
	return result, rejected
}

func viewerSyncFollowingEntries(ctx context.Context, entries []followingEntry) ([]followingEntry, int) {
	result := make([]followingEntry, 0, len(entries))
	rejected := 0
	for _, entry := range entries {
		entry, valid := normalizeFollowingEntry(entry)
		if !valid || !dramaAllowed(ctx, entry.DramaID, entry.Source) {
			rejected++
			continue
		}
		result = append(result, entry)
	}
	return result, rejected
}
