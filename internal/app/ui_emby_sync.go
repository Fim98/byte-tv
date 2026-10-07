package app

import (
	"errors"
	"net/http"
	"sort"
	"time"
)

func (manager *embySyncManager) view() map[string]any {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	settings := manager.document.Settings
	hasKey := settings.APIKey != ""
	settings.APIKey = ""
	entries := make([]map[string]any, 0, len(manager.document.Entries))
	totalEpisodes := 0
	for id, entry := range manager.document.Entries {
		totalEpisodes += entry.Episodes
		entries = append(entries, map[string]any{"dramaId": id, "title": entry.Title, "episodes": entry.Episodes, "merged": entry.Merged,
			"folder": entry.Folder, "syncedAt": entry.SyncedAt, "checkedAt": entry.CheckedAt, "error": entry.Error,
			"posterStatus": entry.Poster.Status, "posterError": entry.Poster.Error, "posterSyncedAt": entry.Poster.SyncedAt,
			"posterFile": entry.Poster.LocalFile, "posterLocalError": entry.Poster.LocalError})
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i]["checkedAt"].(time.Time), entries[j]["checkedAt"].(time.Time)
		if left.Equal(right) {
			return entries[i]["dramaId"].(string) < entries[j]["dramaId"].(string)
		}
		return left.After(right)
	})
	message := manager.document.Error
	if manager.loadErr != nil {
		message = "同步配置读取失败，原文件已保留：" + manager.safeError(manager.loadErr, manager.document.Settings.APIKey)
	}
	return map[string]any{"settings": settings, "hasAPIKey": hasKey, "owner": manager.document.OwnerName,
		"running": manager.running, "lastStartedAt": manager.document.LastStartedAt, "lastFinishedAt": manager.document.LastFinishedAt,
		"nextRunAt": manager.document.NextRunAt, "writtenFiles": manager.document.WrittenFiles, "succeeded": manager.document.Succeeded,
		"failed": manager.document.Failed, "pending": manager.document.Pending, "error": message, "entries": entries, "episodes": totalEpisodes,
		"postersSynced": manager.document.PostersSynced, "postersPending": manager.document.PostersPending,
		"postersFailed": manager.document.PostersFailed, "postersUpdated": manager.document.PostersUpdated}
}

func (app *UIApp) handleEmbySyncSettings(writer http.ResponseWriter, request *http.Request) {
	manager := app.embySyncer()
	if request.Method == http.MethodGet {
		if playbackRequestAllowed(writer, request, http.MethodGet) {
			writeJSON(writer, http.StatusOK, manager.view())
		}
		return
	}
	var input struct {
		Enabled         bool    `json:"enabled"`
		OutputDir       string  `json:"outputDir"`
		BaseURL         string  `json:"baseUrl"`
		IntervalMinutes int     `json:"intervalMinutes"`
		Following       bool    `json:"following"`
		Downloads       bool    `json:"downloads"`
		ServerURL       string  `json:"serverUrl"`
		APIKey          *string `json:"apiKey"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	account, _, _, err := app.browserViewers().requestAccount(request)
	if err != nil || !account.Admin || account.RequirePasswordChange {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请使用管理员账号保存同步设置"})
		return
	}
	manager.mu.Lock()
	if manager.loadErr != nil {
		manager.mu.Unlock()
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "已有 Emby 配置无法读取，请恢复备份后再保存"})
		return
	}
	settings := embySyncSettings{Enabled: input.Enabled, OutputDir: input.OutputDir, BaseURL: input.BaseURL,
		IntervalMinutes: input.IntervalMinutes, Following: input.Following, Downloads: input.Downloads, ServerURL: input.ServerURL}
	if settings.ServerURL != "" {
		if normalized, err := embyBaseURL(settings.ServerURL); err == nil {
			settings.ServerURL = normalized
		}
	}
	if input.APIKey != nil {
		settings.APIKey = *input.APIKey
	} else if settings.ServerURL == manager.document.Settings.ServerURL {
		settings.APIKey = manager.document.Settings.APIKey
	}
	if err = settings.normalize(); err != nil {
		manager.mu.Unlock()
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if manager.closed {
		manager.mu.Unlock()
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "服务正在停止"})
		return
	}
	if settings.Enabled {
		if err = checkOutputDirectory(settings.OutputDir); err != nil {
			manager.mu.Unlock()
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "无法使用此 Emby 输出目录：" + manager.safeError(err, settings.APIKey)})
			return
		}
	}
	document := manager.document
	document.Settings, document.OwnerID, document.OwnerName = settings, account.ID, account.Username
	document.Entries = make(map[string]embySyncEntry, len(manager.document.Entries))
	for id, entry := range manager.document.Entries {
		entry.CheckedAt = time.Time{}
		entry.Poster.CheckedAt, entry.Poster.RetryAt, entry.Poster.Failures = time.Time{}, time.Time{}, 0
		entry.Poster.LocalCheckedAt, entry.Poster.LocalRetryAt = time.Time{}, time.Time{}
		document.Entries[id] = entry
	}
	document.NextRunAt = time.Now()
	document.Error = ""
	if settings.ServerURL == "" {
		document.RefreshPending = false
	}
	if err = writeEmbyJSON(manager.path, document, 0600); err == nil {
		manager.document = document
		manager.generation++
		manager.notify()
		if manager.runCancel != nil {
			manager.runCancel()
		}
	}
	manager.mu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "同步设置保存失败：" + manager.safeError(err, settings.APIKey)})
		return
	}
	manager.start()
	writeJSON(writer, http.StatusOK, manager.view())
}

func (app *UIApp) handleEmbySyncNow(writer http.ResponseWriter, request *http.Request) {
	var input struct{}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	manager := app.embySyncer()
	manager.mu.Lock()
	var err error
	if manager.loadErr != nil {
		err = errors.New("同步配置无法读取，请先恢复备份")
	} else if !manager.document.Settings.Enabled {
		err = errors.New("请先启用并保存 Emby 自动同步")
	} else if manager.closed {
		err = errors.New("服务正在停止")
	}
	if err == nil && !manager.running {
		for id, entry := range manager.document.Entries {
			entry.CheckedAt = time.Time{}
			entry.Poster.CheckedAt, entry.Poster.RetryAt, entry.Poster.Failures = time.Time{}, time.Time{}, 0
			entry.Poster.LocalCheckedAt, entry.Poster.LocalRetryAt = time.Time{}, time.Time{}
			manager.document.Entries[id] = entry
		}
		manager.document.NextRunAt = time.Now()
		manager.notify()
	}
	manager.mu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	manager.start()
	writeJSON(writer, http.StatusAccepted, manager.view())
}
