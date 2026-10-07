package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	"image/png"
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

const embyFixtureCover = "https://p3-shortvideo.byteimg.com/synthetic.png?signature=private-cover-token"

type embyPosterTestTransport func(*http.Request) (*http.Response, error)

func (transport embyPosterTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func embySyntheticPoster(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestEmbyArchiveIncludesSignedPosterMetadataOnly(t *testing.T) {
	drama := Drama{ID: historyFixtureDramaID, Title: "海报 <&>", CoverURL: embyFixtureCover}
	key := bytes.Repeat([]byte{7}, 32)
	body, err := buildEmbyArchive(drama, embySyncTestChapters(), "https://library.test/juku", key, "poster-owner")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 5 {
		t.Fatal("export should contain only STRM and NFO files")
	}
	found := false
	for _, file := range archive.File {
		if !strings.HasSuffix(file.Name, ".strm") && !strings.HasSuffix(file.Name, ".nfo") {
			t.Fatal("export created an artwork or media file", file.Name)
		}
		if !strings.HasSuffix(file.Name, "/tvshow.nfo") {
			continue
		}
		reader, _ := file.Open()
		var nfo struct {
			Title string `xml:"title"`
			Thumb struct {
				URL    string `xml:",chardata"`
				Aspect string `xml:"aspect,attr"`
			} `xml:"thumb"`
			Identity struct {
				Value string `xml:",chardata"`
				Type  string `xml:"type,attr"`
			} `xml:"uniqueid"`
		}
		err = xml.NewDecoder(reader).Decode(&nfo)
		reader.Close()
		if err != nil || nfo.Title != drama.Title || nfo.Thumb.Aspect != "poster" || nfo.Identity.Type != "juku" || nfo.Identity.Value != embyMetadataID(key, drama.ID) {
			t.Fatal("invalid NFO metadata", err)
		}
		cover, err := url.Parse(nfo.Thumb.URL)
		if err != nil || cover.Scheme != "https" || cover.Host != "library.test" || cover.Path != "/juku/api/emby/cover" {
			t.Fatal("cover URL does not preserve the configured reverse proxy path")
		}
		query := cover.Query()
		if query.Get("id") != drama.ID || query.Get("account") != "poster-owner" ||
			query.Get("key") != embyCoverToken(key, drama.ID, "poster-owner", query.Get("v")) || strings.Contains(nfo.Thumb.URL, "private-cover-token") {
			t.Fatal("cover URL is not scoped to this account and drama")
		}
		found = true
	}
	if !found {
		t.Fatal("missing tvshow.nfo")
	}
	drama.CoverURL = "https://untrusted.example.invalid/image"
	if embyCoverURL(drama, "https://library.test", key, "poster-owner") != "" {
		t.Fatal("untrusted source was included in the exported poster URL")
	}
}

func TestEmbyCoverWithoutCookiesChecksSignatureAndCurrentPermissions(t *testing.T) {
	app, admin := administratorFixture(t)
	member := newAccountTestBrowser(t, app)
	member.register(t, "poster-viewer")
	store := app.browserViewers().accountStore()
	owner := store.state.Accounts["poster-viewer"].ID
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/settings", map[string]bool{"requireLogin": true, "allowRegistration": true}))
	app.dramas[0].CoverURL = embyFixtureCover
	data := embySyntheticPoster(t)
	var calls atomic.Int32
	app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.URL.String() != embyFixtureCover || request.Header.Get("Referer") == "" || request.Header.Get("X-Emby-Token") != "" {
			t.Error("cover request lost source headers or leaked Emby credentials")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
	})
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	address := embyCoverURL(app.dramas[0], "http://library.test", key, owner)
	request := func(handler http.Handler, method, address string) *httptest.ResponseRecorder {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(method, address, nil))
		return result
	}
	for _, mutation := range []struct{ name, value string }{
		{"id", "hongguo:7000000000000000002"}, {"account", "another-account"}, {"v", strings.Repeat("0", 32)},
		{"key", embyToken(key, historyFixtureDramaID, "chapter-1", owner)}, {"key", "invalid"},
	} {
		parsed, _ := url.Parse(address)
		query := parsed.Query()
		query.Set(mutation.name, mutation.value)
		parsed.RawQuery = query.Encode()
		if result := request(app.routes(), http.MethodGet, parsed.String()); result.Code != 403 {
			t.Fatal("forged cover was accepted", mutation.name, result.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid signatures reached the source")
	}
	result := request(app.routes(), http.MethodGet, address)
	if result.Code != 200 || !bytes.Equal(result.Body.Bytes(), data) || result.Header().Get("Content-Type") != "image/png" ||
		result.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("signed cover was not usable without browser cookies", result.Code)
	}
	head := request(app.routes(), http.MethodHead, address)
	if head.Code != 200 || head.Body.Len() != 0 || calls.Load() != 1 {
		t.Fatal("cover HEAD/cache failed")
	}
	if result := request(app.routes(), http.MethodPost, address); result.Code != 405 {
		t.Fatal("unsupported cover method accepted")
	}
	restarted := &UIApp{cfg: app.cfg, downloader: app.downloader, dramas: app.dramas}
	if result := request(restarted.routes(), http.MethodGet, address); result.Code != 200 {
		t.Fatal("poster link did not survive restart", result.Code)
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "poster-viewer", "onlineOnly": true}))
	before := calls.Load()
	if result := request(app.routes(), http.MethodGet, address); result.Code != 403 || calls.Load() != before {
		t.Fatal("online-only account retained poster export access")
	}
}

func TestEmbyPosterMatchesIdentityAndPortableFolderPaths(t *testing.T) {
	folder, identity := "同名剧 [0123456789abcdef]", "fixture-provider-id"
	for _, prefix := range []string{"/mnt/emby/", "D:\\Media\\Emby\\"} {
		item, err := matchEmbySeries([]embyMediaItem{
			{ID: "other", Type: "Series", Path: prefix + "同名剧 [fedcba9876543210]"},
			{ID: "wanted", Type: "Series", Path: prefix + folder, ProviderIDs: map[string]string{"Juku": identity}},
		}, folder, identity)
		if err != nil || item.ID != "wanted" {
			t.Fatal("portable folder/identity match failed", item.ID, err)
		}
	}
	items := []embyMediaItem{
		{ID: "first", Type: "Series", Path: "/one/" + folder},
		{ID: "second", Type: "Series", Path: "/two/" + folder},
	}
	if _, err := matchEmbySeries(items, folder, identity); err == nil {
		t.Fatal("ambiguous folders were matched by title")
	}
	items[0].ProviderIDs = map[string]string{"juku": "different-instance"}
	items[1].ID = "../escape"
	if item, err := matchEmbySeries(items, folder, identity); err != nil || item.ID != "" {
		t.Fatal("wrong instance or unsafe item ID accepted")
	}
}

func TestEmbyPostersScanDelayRetryRestartAndCustomArtwork(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/settings", map[string]bool{"requireLogin": true, "allowRegistration": true}))
	app.dramas[0].CoverURL = embyFixtureCover
	data := embySyntheticPoster(t)
	var imageRequests atomic.Int32
	app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "p3-shortvideo.byteimg.com" {
			t.Error("unexpected external request")
			return nil, errors.New("external requests forbidden")
		}
		imageRequests.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
	})
	library := httptest.NewServer(app.routes())
	defer library.Close()
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	folder := embyFolderName(app.dramas[0])
	var ready, failDownload, delayConfirmation atomic.Bool
	var scans, uploads atomic.Int32
	var imageTag atomic.Value
	imageTag.Store("")
	item := func() embyMediaItem {
		return embyMediaItem{ID: "series-1", Type: "Series", Path: "/container/shared/" + folder,
			ProviderIDs: map[string]string{"juku": embyMetadataID(key, historyFixtureDramaID)}, ImageTags: map[string]string{"Primary": imageTag.Load().(string)}}
	}
	emby := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Emby-Token") != "private-api-key" || strings.Contains(request.URL.RawQuery, "private-api-key") {
			t.Error("API key missing from header or exposed in URL")
		}
		switch request.URL.Path {
		case "/emby/Library/Refresh":
			scans.Add(1)
			writer.WriteHeader(204)
		case "/emby/Items":
			result := embyItemList{}
			if ready.Load() && !(request.URL.Query().Get("Ids") != "" && delayConfirmation.Load()) {
				result.Items, result.TotalRecordCount = []embyMediaItem{item()}, 1
			}
			json.NewEncoder(writer).Encode(result)
		case "/emby/Items/series-1/RemoteImages/Download":
			uploads.Add(1)
			if failDownload.Load() {
				http.Error(writer, "sensitive upstream body private-api-key", 503)
				return
			}
			if request.Method != http.MethodPost || request.URL.Query().Get("Type") != "Primary" || request.Header.Get("Content-Type") != "application/json" {
				t.Error("invalid poster download request")
			}
			address := request.URL.Query().Get("ImageUrl")
			if !strings.HasPrefix(address, library.URL+"/api/emby/cover?") {
				t.Error("Emby was sent a source URL instead of the signed application URL")
				writer.WriteHeader(400)
				return
			}
			response, err := http.Get(address)
			if err != nil {
				t.Error(err)
				writer.WriteHeader(502)
				return
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 || !bytes.Equal(body, data) {
				t.Error("Emby could not fetch the synthetic cover without cookies", response.StatusCode)
				writer.WriteHeader(502)
				return
			}
			imageTag.Store("managed-" + request.URL.Query().Get("ImageUrl"))
			writer.WriteHeader(204)
		default:
			t.Error("unexpected Emby request", request.URL.Path)
			writer.WriteHeader(404)
		}
	}))
	defer emby.Close()
	manager.document.OwnerID = app.browserViewers().accountStore().state.Accounts["admin"].ID
	manager.document.Settings = embySyncSettings{Enabled: true, Following: true, IntervalMinutes: 60, OutputDir: t.TempDir(), BaseURL: library.URL, ServerURL: emby.URL + "/emby", APIKey: "private-api-key"}
	forcePoster := func() {
		entry := manager.document.Entries[historyFixtureDramaID]
		entry.Poster.CheckedAt, entry.Poster.RetryAt = time.Time{}, time.Time{}
		manager.document.Entries[historyFixtureDramaID] = entry
	}
	run := func() embySyncEntry {
		manager.runOnce(context.Background())
		return manager.document.Entries[historyFixtureDramaID]
	}
	first := run()
	if first.Episodes != 3 || first.Error != "" || first.Poster.Status != "waiting" || scans.Load() != 1 || uploads.Load() != 0 ||
		manager.document.PostersPending != 1 || time.Until(manager.document.NextRunAt) > 2*time.Minute {
		t.Fatal("first scan did not queue a durable poster retry", first.Poster.Status, manager.document.Error)
	}
	file := filepath.Join(manager.document.Settings.OutputDir, first.Folder, "Season 01", "S01E001.strm")
	before, _ := os.Stat(file)
	ready.Store(true)
	forcePoster()
	second := run()
	after, _ := os.Stat(file)
	if second.Poster.Status != "synced" || uploads.Load() != 1 || imageRequests.Load() != 1 || manager.document.WrittenFiles != 0 ||
		scans.Load() != 1 || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("poster retry rewrote/refetched episodes or did not synchronize", second.Poster.Status, manager.document.Error)
	}
	forcePoster()
	run()
	if uploads.Load() != 1 {
		t.Fatal("unchanged artwork was downloaded again")
	}
	restarted := &UIApp{cfg: app.cfg, downloader: app.downloader, dramas: app.dramas}
	manager = restarted.embySyncer()
	if manager.loadErr != nil || manager.document.Entries[historyFixtureDramaID].Poster.ImageTag == "" {
		t.Fatal("restart lost managed artwork state", manager.loadErr)
	}
	forcePoster()
	run()
	if uploads.Load() != 1 || scans.Load() != 1 {
		t.Fatal("restart duplicated existing artwork or files")
	}
	setCover := func(address string) {
		app.mu.Lock()
		app.dramas = append([]Drama(nil), app.dramas...)
		app.dramas[0].CoverURL = address
		dramas := append([]Drama(nil), app.dramas...)
		app.mu.Unlock()
		restarted.mu.Lock()
		restarted.dramas = dramas
		restarted.mu.Unlock()
	}
	setCover(embyFixtureCover + "&revision=2")
	failDownload.Store(true)
	failed := run()
	if failed.Error != "" || failed.Poster.Status != "failed" || manager.document.PostersFailed != 1 ||
		!strings.Contains(failed.Poster.Error, "HTTP 503") || time.Until(failed.Poster.RetryAt) < 30*time.Second {
		t.Fatal("poster failure affected episode import or lost retry state", failed.Poster.Status, manager.document.Error)
	}
	serialized, _ := json.Marshal(manager.view())
	if bytes.Contains(serialized, []byte("private-api-key")) || bytes.Contains(serialized, []byte("private-cover-token")) {
		t.Fatal("sync status exposed private URLs or credentials")
	}
	count := uploads.Load()
	run()
	if uploads.Load() != count {
		t.Fatal("failed artwork ignored backoff")
	}
	failDownload.Store(false)
	delayConfirmation.Store(true)
	forcePoster()
	unconfirmed := run()
	if unconfirmed.Poster.SyncedSource == "" || unconfirmed.Poster.Status != "failed" {
		t.Fatal("successful submission with delayed confirmation was lost")
	}
	count = uploads.Load()
	delayConfirmation.Store(false)
	imageTag.Store("custom-during-confirmation-failure")
	forcePoster()
	if confirmed := run(); confirmed.Poster.Status != "existing" || confirmed.Poster.ImageTag != "" || uploads.Load() != count {
		t.Fatal("unconfirmed image ownership overwrote or adopted custom artwork")
	}
	imageTag.Store("user-custom-cover")
	forcePoster()
	if custom := run(); custom.Poster.Status != "existing" || uploads.Load() != count {
		t.Fatal("custom artwork was overwritten")
	}
	setCover(embyFixtureCover + "&revision=3")
	if custom := run(); custom.Poster.Status != "existing" || uploads.Load() != count {
		t.Fatal("metadata update overwrote custom artwork")
	}
	imageTag.Store("")
	forcePoster()
	if repaired := run(); repaired.Poster.Status != "synced" || uploads.Load() != count+1 {
		t.Fatal("missing artwork was not repaired")
	}
	err = filepath.WalkDir(manager.document.Settings.OutputDir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && !strings.HasSuffix(path, ".strm") && !strings.HasSuffix(path, ".nfo") && entry.Name() != ".juku-emby.json" && entry.Name() != "poster.png" {
			t.Error("synchronization wrote an unexpected file into the export directory", entry.Name())
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(manager.document.Settings.OutputDir, folder, "poster.png")
	if body, err := os.ReadFile(local); err != nil || !bytes.Equal(body, data) {
		t.Fatal("automatic sync did not preserve the local poster", err)
	}
}

func TestEmbyPosterAPIDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		forwarded.Add(1)
		writer.WriteHeader(200)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client := newEmbyAPIClient(embySyncSettings{ServerURL: redirect.URL, APIKey: "private-api-key"})
	defer client.close()
	if _, err := client.series(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 302") || forwarded.Load() != 0 {
		t.Fatal("Emby request followed a redirect with credentials", err)
	}
}
