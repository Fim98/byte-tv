package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
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

func TestEmbySourceGroupingKeepsOldFoldersAndStableLinks(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Source: sourceHongguo, Title: "同名剧"}
	key := bytes.Repeat([]byte{9}, 32)
	ctx := context.Background()
	flat, _, _, err := syncEmbyDramaFiles(ctx, settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(settings.OutputDir, flat, "Season 01", "S01E001.strm")
	before, _ := os.ReadFile(first)
	settings.GroupBySource = true
	drama.Title = "修改后的标题"

	same, _, _, err := syncEmbyDramaFiles(ctx, settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil || same != flat || strings.Contains(same, "/") {
		t.Fatal("grouping or title change duplicated an old export", same, err)
	}
	after, _ := os.ReadFile(first)
	if !bytes.Equal(before, after) {
		t.Fatal("grouping changed a previous episode link")
	}
	for _, id := range []string{"hongguo:7000000000000000002", "hongguo:7000000000000000003"} {
		newDrama := Drama{ID: id, Source: sourceHongguo, Title: "同名剧"}
		folder, _, _, err := syncEmbyDramaFiles(ctx, settings, newDrama, embySyncTestChapters(), "", key, "owner")
		if err != nil || !strings.HasPrefix(folder, dramaSourceFolder(newDrama)+"/") {
			t.Fatal("new export did not use its source folder", folder, err)
		}
		settings.GroupBySource = false
		newDrama.Title = "新的资料标题"
		recovered, _, _, err := syncEmbyDramaFiles(ctx, settings, newDrama, embySyncTestChapters(), "", key, "owner")
		if err != nil || recovered != folder {
			t.Fatal("switching grouping off moved an existing export", recovered, err)
		}
		item, err := matchEmbySeries([]embyMediaItem{{ID: "matched", Type: "Series", Path: "D:\\Emby\\" + strings.ReplaceAll(folder, "/", "\\")}}, folder, "")
		if err != nil || item.ID != "matched" {
			t.Fatal("nested Windows Emby path did not match", err)
		}
		settings.GroupBySource = true
	}
	settings.OutputDir = t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(settings.OutputDir, "红果")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, _, _, err := syncEmbyDramaFiles(ctx, settings, drama, embySyncTestChapters(), "", key, "owner"); err == nil {
		t.Fatal("followed a source-folder symlink")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("wrote outside the export root")
	}
}

func TestEmbyMergedArchiveSignedRangesAndPermissions(t *testing.T) {
	app, admin := administratorFixture(t)
	member := newAccountTestBrowser(t, app)
	member.register(t, "merged-viewer")
	owner := app.browserViewers().accountStore().state.Accounts["merged-viewer"].ID
	drama := app.dramas[0]
	body := bytes.Repeat([]byte("synthetic-merged-media"), 30)
	path := filepath.Join(t.TempDir(), "merged.mp4")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	app.merges = map[string]*UIMergeState{drama.ID: {DramaID: drama.ID, DramaTitle: drama.Title, Status: "success",
		StartEpisode: 1, EndEpisode: 88, Merged: 88, OutputPath: path}}
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	record := app.embyMergedRecord(drama.ID)
	folder := "红果/" + embyFolderName(drama)
	archiveBody, err := buildEmbyArchiveInFolder(drama, embySyncTestChapters(), "http://library.test", key, owner, folder, record)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(archiveBody), int64(len(archiveBody)))
	if err != nil || len(archive.File) != 7 {
		t.Fatal("archive lost ordinary episodes or the merged special", err)
	}
	var address string
	for _, file := range archive.File {
		if !strings.HasPrefix(file.Name, folder+"/") {
			t.Fatal("archive escaped the source folder", file.Name)
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(reader)
		reader.Close()
		if strings.HasSuffix(file.Name, "Season 00/S00E001.strm") {
			address = strings.TrimSpace(string(data))
		}
		if strings.HasSuffix(file.Name, "Season 00/S00E001.nfo") &&
			(!bytes.Contains(data, []byte("<season>0</season>")) || !bytes.Contains(data, []byte("第1–88集"))) {
			t.Fatal("merged special has incorrect NFO")
		}
	}
	if address == "" {
		t.Fatal("merged STRM missing")
	}
	request := func(target *UIApp, method, link, span string) *httptest.ResponseRecorder {
		writer := httptest.NewRecorder()
		req := httptest.NewRequest(method, link, nil)
		req.Header.Set("Range", span)
		target.routes().ServeHTTP(writer, req)
		return writer
	}
	for _, test := range []struct {
		method, span string
		code, length int
	}{{"GET", "bytes=0-15", 206, 16}, {"GET", "bytes=-16", 206, 16}, {"HEAD", "", 200, 0}, {"GET", "bytes=99999-", 416, -1}, {"POST", "", 405, -1}} {
		result := request(app, test.method, address, test.span)
		if result.Code != test.code || test.length >= 0 && result.Body.Len() != test.length {
			t.Fatal("merged media failed an Emby range/probe request", test, result.Code, result.Body.String())
		}
	}
	app.statePath = filepath.Join(t.TempDir(), "ui-state.json")
	if err := app.saveStateLocked(); err != nil {
		t.Fatal(err)
	}
	restarted := &UIApp{cfg: app.cfg, downloader: app.downloader, statePath: app.statePath, tasks: map[string]*UITask{}}
	restarted.loadState()
	if result := request(restarted, "GET", address, "bytes=0-15"); result.Code != 206 || !bytes.Equal(result.Body.Bytes(), body[:16]) {
		t.Fatal("merged link did not survive restart without episode tasks", result.Code)
	}
	for _, field := range []string{"key", "account", "chapter", "id"} {
		parsed, _ := url.Parse(address)
		query := parsed.Query()
		query.Set(field, "forged")
		parsed.RawQuery = query.Encode()
		if result := request(app, "GET", parsed.String(), ""); result.Code != 403 {
			t.Fatal("forged merged link accepted", field, result.Code)
		}
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "merged-viewer", "onlineOnly": true}))
	if result := request(app, "GET", address, ""); result.Code != 403 {
		t.Fatal("revoked export permission retained merged access", result.Code)
	}
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal(err)
	}
	if app.embyMergedRecord(drama.ID) != nil {
		t.Fatal("missing merged file was offered for export")
	}
}

func TestEmbyMergeCompletionWakesSyncAndMergedOnlyLibrary(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	manager.document.OwnerID = app.browserViewers().accountStore().state.Accounts["admin"].ID
	manager.document.Settings = embySyncSettings{Enabled: true, Following: true, IntervalMinutes: 60, OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	manager.runOnce(context.Background())
	entry := manager.document.Entries[historyFixtureDramaID]
	first := filepath.Join(manager.document.Settings.OutputDir, entry.Folder, "Season 01", "S01E001.strm")
	before, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "merged.mp4")
	if err := os.WriteFile(path, []byte("completed media fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	app.statePath = filepath.Join(t.TempDir(), "ui-state.json")
	app.setMergeState(uiMergeResult{DramaID: historyFixtureDramaID, DramaTitle: "已合并短剧",
		StartEpisode: 1, EndEpisode: 3, Merged: 3, OutputPath: path}, "success", 100, false, false)
	manager.runOnce(context.Background())
	entry = manager.document.Entries[historyFixtureDramaID]
	if !entry.Merged || entry.Episodes != 3 || manager.document.WrittenFiles != 2 {
		t.Fatal("recently synced drama did not receive the completed merge", entry, manager.document.WrittenFiles)
	}
	if after, _ := os.ReadFile(first); !bytes.Equal(before, after) {
		t.Fatal("adding a merged version altered the existing episode link")
	}
	broken := app.downloader.hongguoClient().details["7000000000000000001"]
	broken.Chapters = nil
	app.downloader.hongguoClient().details["7000000000000000001"] = broken
	app.tasks, app.taskOrder, app.dramas = nil, nil, nil
	manager.document.Settings.Following, manager.document.Settings.Downloads = false, true
	manager.document.Settings.OutputDir = t.TempDir()
	manager.document.Entries = map[string]embySyncEntry{}
	app.cfg.GroupBySource = true
	manager.runOnce(context.Background())
	entry = manager.document.Entries[historyFixtureDramaID]
	if entry.Error != "" || !entry.Merged || entry.Episodes != 0 || !strings.HasPrefix(entry.Folder, "红果/") {
		t.Fatal("merged-only drama was omitted from downloads sync", entry, manager.document.Error)
	}
	if _, err := os.Stat(filepath.Join(manager.document.Settings.OutputDir, entry.Folder, "Season 00", "S00E001.strm")); err != nil {
		t.Fatal("merged-only STRM missing", err)
	}
}

func TestEmbyMergedExportSurvivesUnavailableSource(t *testing.T) {
	app, admin := administratorFixture(t)
	drama := app.dramas[0]
	path := filepath.Join(t.TempDir(), "merged.mp4")
	if err := os.WriteFile(path, []byte("completed local merge"), 0600); err != nil {
		t.Fatal(err)
	}
	app.merges = map[string]*UIMergeState{drama.ID: {DramaID: drama.ID, DramaTitle: drama.Title,
		Status: "success", StartEpisode: 1, EndEpisode: 3, OutputPath: path}}
	client := app.downloader.hongguoClient()
	client.mu.Lock()
	delete(client.details, "7000000000000000001")
	client.mu.Unlock()
	var requests atomic.Int32
	app.downloader.client = &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return rankingHTTPResponse(request, http.StatusServiceUnavailable, "synthetic unavailable source"), nil
	})}
	response := admin.request(t, http.MethodPost, "/api/emby/export", map[string]any{"dramaId": drama.ID, "baseUrl": "http://library.test"})
	if response.Code != http.StatusOK || requests.Load() == 0 {
		t.Fatal("local merged export depended on source availability", response.Code, response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil || len(archive.File) != 3 {
		t.Fatal("merged-only ZIP must contain the show NFO and merged STRM/NFO", err)
	}
	manager := app.embySyncer()
	manager.document.OwnerID = app.browserViewers().accountStore().state.Accounts["admin"].ID
	manager.document.Settings = embySyncSettings{Enabled: true, Downloads: true, IntervalMinutes: 60, OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	manager.runOnce(context.Background())
	entry := manager.document.Entries[drama.ID]
	if entry.Error != "" || !entry.Merged || entry.Episodes != 0 {
		t.Fatal("automatic sync lost an offline local merge", entry)
	}
	if _, err := os.Stat(filepath.Join(manager.document.Settings.OutputDir, entry.Folder, "Season 00", "S00E001.strm")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.embyExportChapters(context.Background(), drama.ID, nil); err == nil {
		t.Fatal("source failure was hidden without a local merge")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := app.embyExportChapters(ctx, drama.ID, app.embyMergedRecord(drama.ID)); !errors.Is(err, context.Canceled) {
		t.Fatal("merged export ignored cancellation", err)
	}
}

func TestEmbyLocalPosterIsStableAndPreservesCustomArtwork(t *testing.T) {
	app := viewerTestApp(t)
	drama := app.dramas[0]
	drama.CoverURL = embyFixtureCover
	settings := embySyncSettings{GroupBySource: true, OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	ctx := context.Background()
	key := bytes.Repeat([]byte{8}, 32)
	folder, _, _, err := syncEmbyDramaFiles(ctx, settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	data := embySyntheticPoster(t)
	var requests atomic.Int32
	app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
	})
	name, changed, err := app.syncLocalEmbyPoster(ctx, settings, drama, folder, embyFixtureCover)
	if err != nil || !changed || name != "poster.png" || !strings.HasPrefix(folder, "红果/") {
		t.Fatal("local artwork did not join the grouped media directory", name, changed, err)
	}
	posterPath := filepath.Join(settings.OutputDir, filepath.FromSlash(folder), name)
	if _, _, _, err := syncEmbyDramaFiles(ctx, settings, drama, embySyncTestChapters(), folder, key, "owner", &embyMergedRecord{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := app.syncLocalEmbyPoster(ctx, settings, drama, folder, embyFixtureCover); err != nil || changed || requests.Load() != 1 {
		t.Fatal("core sync lost artwork ownership or rewrote the poster", err)
	}
	custom := []byte("user-edited poster")
	if err := os.WriteFile(posterPath, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := app.syncLocalEmbyPoster(ctx, settings, drama, folder, embyFixtureCover+"&revision=2"); err != nil || changed || requests.Load() != 1 {
		t.Fatal("user-modified poster was overwritten", err)
	}
	if body, _ := os.ReadFile(posterPath); !bytes.Equal(body, custom) {
		t.Fatal("custom image bytes changed")
	}

	settings.OutputDir = t.TempDir()
	folder, _, _, err = syncEmbyDramaFiles(ctx, settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	posterPath = filepath.Join(settings.OutputDir, filepath.FromSlash(folder), "poster.png")
	app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
		if err := os.WriteFile(posterPath, custom, 0600); err != nil {
			t.Error(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
	})
	if _, changed, err := app.syncLocalEmbyPoster(ctx, settings, drama, folder, embyFixtureCover+"&revision=during-fetch"); err != nil || changed {
		t.Fatal("artwork added during fetch was replaced", err)
	}
	if body, _ := os.ReadFile(posterPath); !bytes.Equal(body, custom) {
		t.Fatal("fetch raced with user artwork")
	}
}

func TestEmbyLocalPosterFailureRetriesWithoutLosingSTRM(t *testing.T) {
	app := viewerTestApp(t)
	manager := app.embySyncer()
	drama := app.dramas[0]
	drama.CoverURL = embyFixtureCover
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test", IntervalMinutes: 60}
	folder, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", make([]byte, 32), "owner")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	app.downloader.client.Transport = embyPosterTestTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("fixture offline")
	})
	document := embySyncDocument{Settings: settings, Entries: map[string]embySyncEntry{drama.ID: {Folder: folder, Episodes: 2}}}
	manager.preparePosters(&document, []Drama{drama}, make([]byte, 32))
	manager.synchronizeLocalPosters(context.Background(), &document, []Drama{drama})
	poster := document.Entries[drama.ID].Poster
	if poster.LocalError == "" || poster.Status != "failed" || time.Until(poster.LocalRetryAt) < 4*time.Minute {
		t.Fatal("offline poster lost bounded retry state", poster)
	}
	manager.synchronizeLocalPosters(context.Background(), &document, []Drama{drama})
	if calls.Load() != 1 {
		t.Fatal("failed image was fetched before retry time")
	}
	if _, err := os.Stat(filepath.Join(settings.OutputDir, folder, "Season 01", "S01E001.strm")); err != nil {
		t.Fatal("poster failure damaged STRM", err)
	}
	poster.LocalFile, poster.LocalSource = "poster.png", embyPosterSourceHash(poster.SourceURL)
	poster.LocalRetryAt = time.Now().Add(-time.Second)
	if next := embyLocalPosterDue(poster, time.Hour); next.After(time.Now()) {
		t.Fatal("an existing poster suppressed the scheduled repair")
	}
}
