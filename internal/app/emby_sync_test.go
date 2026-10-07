package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func embySyncTestChapters() []Chapter {
	return []Chapter{{ID: "fixture-1", Title: "第一集"}, {ID: "fixture-2", Title: "第二集"}}
}

func TestEmbySyncFilesIdempotentStableAndNonDestructive(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test:8998"}
	drama := Drama{ID: historyFixtureDramaID, Title: "合成测试 <&>"}
	key := make([]byte, 32)
	chapters := embySyncTestChapters()
	folder, written, total, err := syncEmbyDramaFiles(context.Background(), settings, drama, chapters, "", key, "fixture-owner")
	if err != nil || written != 5 || total != 2 {
		t.Fatal(folder, written, total, err)
	}
	first := filepath.Join(settings.OutputDir, folder, "Season 01", "S01E001.strm")
	before, _ := os.Stat(first)
	original, _ := os.ReadFile(first)
	if !strings.Contains(string(original), "account=fixture-owner") {
		t.Fatal("automated link not bound to owner")
	}
	_, written, _, err = syncEmbyDramaFiles(context.Background(), settings, drama, chapters, folder, key, "fixture-owner")
	after, _ := os.Stat(first)
	if err != nil || written != 0 || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged export rewrote files", written, err)
	}
	note := filepath.Join(settings.OutputDir, folder, "my-notes.txt")
	if err = os.WriteFile(note, []byte("user text"), 0600); err != nil {
		t.Fatal(err)
	}
	chapters = []Chapter{chapters[1], chapters[0], {ID: "fixture-3", Title: "第三集"}}
	drama.Title = "资料更新后的合成剧名"
	same, written, total, err := syncEmbyDramaFiles(context.Background(), settings, drama, chapters, folder, key, "fixture-owner")
	if err != nil || same != folder || total != 3 {
		t.Fatal("renamed/reordered series duplicated or renumbered", same, total, err)
	}
	current, _ := os.ReadFile(first)
	if string(current) != string(original) {
		t.Fatal("existing episode link changed after source order changed")
	}
	if body, _ := os.ReadFile(note); string(body) != "user text" {
		t.Fatal("unrelated file was changed")
	}
	_, _, total, err = syncEmbyDramaFiles(context.Background(), settings, drama, chapters[:1], folder, key, "fixture-owner")
	if err != nil || total != 3 {
		t.Fatal("temporarily shorter upstream deleted previous episodes", total, err)
	}
	marker := filepath.Join(settings.OutputDir, folder, ".juku-emby.json")
	var manifest embyFolderManifest
	if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil || len(manifest.Chapters) != 3 {
		t.Fatal(err)
	}
}

func TestEmbySyncRefusesUnmanagedFoldersAndSymlinks(t *testing.T) {
	root := t.TempDir()
	settings := embySyncSettings{OutputDir: root, BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "独立目录"}
	folder := embyFolderName(drama)
	if err := os.Mkdir(filepath.Join(root, folder), 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, folder, "sentinel.txt")
	os.WriteFile(sentinel, []byte("preserved"), 0600)
	if _, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", make([]byte, 32), "owner"); err == nil {
		t.Fatal("unmanaged directory adopted without permission")
	}
	if data, _ := os.ReadFile(sentinel); string(data) != "preserved" {
		t.Fatal("unmanaged directory modified")
	}
	settings.OutputDir = t.TempDir()
	folder, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", make([]byte, 32), "owner")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(settings.OutputDir, folder, "Season 01", "S01E001.strm")
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(sentinel, target); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, _, _, err = syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), folder, make([]byte, 32), "owner"); err == nil {
		t.Fatal("managed file symlink followed")
	}
	if data, _ := os.ReadFile(sentinel); string(data) != "preserved" {
		t.Fatal("symlink target modified")
	}
	for _, folder := range []string{"../escape", "..", "/absolute", "nested/path", "nested\\path"} {
		if _, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), folder, make([]byte, 32), "owner"); err == nil {
			t.Fatal("unsafe folder accepted", folder)
		}
	}
}

func waitEmbySync(t *testing.T, manager *embySyncManager, after time.Time) embySyncDocument {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		done := !manager.running && manager.document.LastFinishedAt.After(after)
		snapshot := manager.document
		manager.mu.Unlock()
		if done {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("automatic Emby synchronization did not finish")
	return embySyncDocument{}
}

func TestEmbySyncAutomaticFollowedImportRefreshAndRestart(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	ordinary := newAccountTestBrowser(t, app)
	ordinary.register(t, "other-viewer")
	otherID := "hongguo:7000000000000000002"
	app.dramas = append(app.dramas, Drama{ID: otherID, Source: sourceHongguo, Title: "另一个用户的剧"})
	viewerResultOK(t, ordinary.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": otherID, "saved": true}))
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	var scans atomic.Int32
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emby/Library/Refresh" || r.Header.Get("X-Emby-Token") != "private-test-key" || r.URL.RawQuery != "" {
			t.Error("invalid Emby refresh request")
		}
		scans.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer emby.Close()
	settings := map[string]any{"enabled": true, "outputDir": t.TempDir(), "baseUrl": "http://library.test:8998", "intervalMinutes": 60, "following": true, "downloads": false, "serverUrl": emby.URL + "/emby", "apiKey": "private-test-key"}
	started := time.Now()
	response := admin.request(t, http.MethodPost, "/api/ui/admin/emby", settings)
	viewerResultOK(t, response)
	if strings.Contains(response.Body.String(), "private-test-key") {
		t.Fatal("API response leaked the Emby key")
	}
	result := waitEmbySync(t, manager, started)
	if result.Error != "" || result.Succeeded != 1 || len(result.Entries) != 1 || result.Entries[historyFixtureDramaID].Episodes != 3 || scans.Load() != 1 {
		t.Fatal("automatic import failed or crossed accounts", result.Error, result.Succeeded, len(result.Entries), scans.Load())
	}
	output := settings["outputDir"].(string)
	entry := result.Entries[historyFixtureDramaID]
	strm, err := os.ReadFile(filepath.Join(output, entry.Folder, "Season 01", "S01E001.strm"))
	if err != nil {
		t.Fatal(err)
	}
	link := strings.TrimSpace(string(strm))
	parsed, _ := url.Parse(link)
	if parsed.Query().Get("account") == "" {
		t.Fatal("automatic export created a guest link")
	}
	writer := httptest.NewRecorder()
	app.handleEmbyStream(writer, httptest.NewRequest(http.MethodHead, link, nil))
	if writer.Code != 200 {
		t.Fatal("automatic signed link unusable", writer.Code)
	}
	info, err := os.Stat(manager.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("sync settings not saved privately")
	}
	manager.stop()
	restarted := &UIApp{cfg: app.cfg, downloader: app.downloader, dramas: app.dramas}
	next := restarted.embySyncer()
	if next.loadErr != nil || !next.document.Settings.Enabled || next.document.Settings.APIKey != "private-test-key" {
		t.Fatal("restart lost configuration", next.loadErr)
	}
	for id, entry := range next.document.Entries {
		entry.CheckedAt = time.Time{}
		next.document.Entries[id] = entry
	}
	next.runOnce(context.Background())
	if next.document.WrittenFiles != 0 || scans.Load() != 1 || next.document.Error != "" {
		t.Fatal("restart duplicated exports or scans", next.document.WrittenFiles, scans.Load(), next.document.Error)
	}
	if data, err := os.ReadFile(filepath.Join(output, entry.Folder, "Season 01", "S01E001.strm")); err != nil || string(data) != string(strm) {
		t.Fatal("restart changed stable link")
	}
}

func TestEmbySyncAdminGateDisabledDefaultsAndValidation(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	guest := newAccountTestBrowser(t, app)
	member := newAccountTestBrowser(t, app)
	member.register(t, "online-only-fixture")
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "online-only-fixture", "onlineOnly": true}))
	for _, browser := range []*accountTestBrowser{guest, member} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := browser.request(t, method, "/api/ui/admin/emby", map[string]any{})
			if response.Code != http.StatusForbidden {
				t.Fatal("non-admin accessed server export settings", response.Code)
			}
		}
	}
	response := admin.request(t, http.MethodGet, "/api/ui/admin/emby", nil)
	viewerResultOK(t, response)
	if manager.document.Settings.Enabled {
		t.Fatal("automatic export enabled by default")
	}
	if _, err := os.Stat(manager.path); !os.IsNotExist(err) {
		t.Fatal("reading settings created an export job")
	}
	input := map[string]any{"enabled": true, "outputDir": t.TempDir(), "baseUrl": "file:///private/fixture", "intervalMinutes": 60, "following": true, "downloads": false}
	if result := admin.request(t, http.MethodPost, "/api/ui/admin/emby", input); result.Code != http.StatusBadRequest {
		t.Fatal("invalid playback address accepted", result.Code)
	}
	input["baseUrl"] = "http://library.test"
	input["intervalMinutes"] = 1
	if result := admin.request(t, http.MethodPost, "/api/ui/admin/emby", input); result.Code != http.StatusBadRequest {
		t.Fatal("unbounded refresh frequency accepted", result.Code)
	}
	input["intervalMinutes"] = 60
	input["enabled"] = false
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/emby", input))
	if result := admin.request(t, http.MethodPost, "/api/ui/admin/emby/sync", map[string]any{}); result.Code != http.StatusConflict {
		t.Fatal("disabled sync executed", result.Code)
	}
}

func TestEmbySyncPartialFailurePreservesFilesAndRetriesScan(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	owner := app.browserViewers().accountStore().state.Accounts["admin"]
	var scans atomic.Int32
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := scans.Add(1)
		if count == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer emby.Close()
	manager.document.Settings = embySyncSettings{Enabled: true, OutputDir: t.TempDir(), BaseURL: "http://library.test", IntervalMinutes: 60, Following: true, ServerURL: emby.URL, APIKey: "scan-secret"}
	manager.document.OwnerID = owner.ID
	manager.runOnce(context.Background())
	if !manager.document.RefreshPending || !strings.Contains(manager.document.Error, "HTTP 503") {
		t.Fatal("failed scan was not retained for retry", manager.document.Error)
	}
	entry := manager.document.Entries[historyFixtureDramaID]
	before, _ := os.ReadFile(filepath.Join(manager.document.Settings.OutputDir, entry.Folder, "Season 01", "S01E001.strm"))
	manager.runOnce(context.Background())
	if manager.document.RefreshPending || scans.Load() != 2 || manager.document.WrittenFiles != 0 {
		t.Fatal("scan retry rewrote files or was skipped")
	}
	client := app.downloader.hongguoClient()
	broken := client.details["7000000000000000001"]
	broken.Chapters = nil
	client.details["7000000000000000001"] = broken
	entry.CheckedAt = time.Time{}
	manager.document.Entries[historyFixtureDramaID] = entry
	manager.runOnce(context.Background())
	if manager.document.Failed != 1 || manager.document.Entries[historyFixtureDramaID].Episodes != 3 {
		t.Fatal("upstream failure lost previous imported episodes")
	}
	after, _ := os.ReadFile(filepath.Join(manager.document.Settings.OutputDir, entry.Folder, "Season 01", "S01E001.strm"))
	if string(before) != string(after) {
		t.Fatal("failed source response overwrote completed export")
	}
	state, _ := json.Marshal(manager.view())
	if strings.Contains(string(state), "scan-secret") {
		t.Fatal("status leaked API key")
	}
}

func TestEmbySyncDownloadedCollectionAndLocalPlaybackPriority(t *testing.T) {
	app, _ := administratorFixture(t)
	owner := app.browserViewers().accountStore().state.Accounts["admin"]
	cached := app.downloader.hongguoClient().details["7000000000000000001"]
	path := filepath.Join(t.TempDir(), "local-fixture.mp4")
	if err := os.WriteFile(path, []byte("synthetic file identity only"), 0600); err != nil {
		t.Fatal(err)
	}
	task := Task{DramaID: historyFixtureDramaID, DramaTitle: "下载完成的合成剧", Chapter: cached.Chapters[0], OutPath: path, Index: 1, Total: 3}
	app.tasks = map[string]*UITask{"download-fixture": {ID: "download-fixture", DramaID: historyFixtureDramaID, DramaTitle: task.DramaTitle, Path: path, Status: uiStatusSuccess, Source: task}}
	manager := app.embySyncer()
	manager.document.OwnerID = owner.ID
	manager.document.Settings = embySyncSettings{Enabled: true, OutputDir: t.TempDir(), BaseURL: "http://library.test", IntervalMinutes: 60, Downloads: true}
	manager.runOnce(context.Background())
	if manager.document.Error != "" || manager.document.Entries[historyFixtureDramaID].Episodes != 3 {
		t.Fatal("download collection not imported", manager.document.Error)
	}
	local, downloadID, err := app.embyTask(context.Background(), historyFixtureDramaID, task.Chapter.ID)
	if err != nil || downloadID != "download-fixture" || local.OutPath != path {
		t.Fatal("Emby did not prefer completed local file", downloadID, err)
	}
	firstRun := manager.document.LastFinishedAt
	nextRun := manager.document.NextRunAt
	manager.runOnce(context.Background())
	if manager.document.LastFinishedAt != firstRun || manager.document.NextRunAt.After(nextRun.Add(time.Second)) {
		t.Fatal("unrelated notification postponed scheduled updates")
	}
}

func TestEmbySyncStartupDoesNotImmediatelyRepeatFailedScan(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	scans := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { scans <- struct{}{}; w.WriteHeader(503) }))
	defer server.Close()
	started := time.Now()
	input := map[string]any{"enabled": true, "outputDir": t.TempDir(), "baseUrl": "http://library.test", "intervalMinutes": 60, "following": true, "downloads": false, "serverUrl": server.URL, "apiKey": "fixture-scan-key"}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/emby", input))
	result := waitEmbySync(t, manager, started)
	if result.Succeeded != 1 || !result.RefreshPending || !strings.Contains(result.Error, "HTTP 503") {
		t.Fatal("failed scan status not preserved", result.Error)
	}
	<-scans
	select {
	case <-scans:
		t.Fatal("startup and settings wake triggered duplicate immediate scan")
	case <-time.After(500 * time.Millisecond):
	}
}
