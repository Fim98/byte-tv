package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitSortMetadataQueue(t *testing.T, app *UIApp) {
	t.Helper()
	app.mu.Lock()
	done := app.metadataDone
	app.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("metadata queue did not finish")
		}
	}
}

func TestPrimaryCatalogReturnsBeforeOptionalMetadata(t *testing.T) {
	for _, source := range []string{sourceHongguo} {
		t.Run(source, func(t *testing.T) {
			id := "7615465407347952664"
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var calls atomic.Int32
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				if request.Context().Value(backgroundCatalogKey{}) != true {
					t.Error("optional detail did not use background priority")
				}
				once.Do(func() { close(started) })
				select {
				case <-release:
				case <-request.Context().Done():
					return nil, request.Context().Err()
				}
				switch request.URL.Path {
				case "/detail":
					if request.URL.Query().Get("series_id") != id {
						t.Error("historical entry was automatically fetched")
					}
					return rankingHTTPResponse(request, 200, sortDetailFixture(id, "1773662280")), nil
				case "/novel/player/video_detail/v1/":
					return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"video_data":{"series_id_str":%q,"series_title":"详情标题","hot_score":20019,"series_play_cnt":10019}}}`, id)), nil
				default:
					t.Errorf("unexpected metadata request: %s", request.URL.Path)
					return rankingHTTPResponse(request, 404, ""), nil
				}
			})
			old := Drama{ID: providerDramaID(source, "7498612570866076734"), Source: source, Title: "历史条目"}
			fresh := Drama{ID: providerDramaID(source, id), Source: source, Title: "列表标题", Heat: "20019", Views: "0", Cover: coverAddressFixture}
			app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old}, librarySources: map[string]librarySourceState{}, libraryLoading: make(chan struct{})}
			t.Cleanup(app.stopSortMetadata)
			app.acceptLibraryProgress(source, []Drama{old, fresh}, nil, false)
			app.mu.Lock()
			if calls.Load() != 0 || len(app.dramas) != 2 || app.dramas[1].Heat != fresh.Heat || app.dramas[1].Views != "0" {
				t.Error("primary fields did not return before optional requests")
			}
			close(app.libraryLoading)
			app.libraryLoading = nil
			app.mu.Unlock()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("new entry was not scheduled automatically")
			}

			app.acceptLibraryProgress(source, []Drama{fresh, old}, nil, true)
			writer := httptest.NewRecorder()
			app.handleDramas(writer, httptest.NewRequest("GET", "/api/ui/dramas", nil))
			var snapshot struct {
				Loading  bool                    `json:"loading"`
				Metadata libraryMetadataProgress `json:"metadata"`
			}
			if json.Unmarshal(writer.Body.Bytes(), &snapshot) != nil || snapshot.Loading || !snapshot.Metadata.Running || snapshot.Metadata.Total != 1 {
				t.Fatal("optional metadata blocked the library or duplicated work")
			}
			close(release)
			waitSortMetadataQueue(t, app)
			cache, err := readLibraryCache(d.cfg.dataDirectory())
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range cache.Dramas {
				if row.ID == old.ID && row.SortMetadata != nil {
					t.Error("ordinary catalog refresh enrolled historical data")
				}
				wantViews := "10019次播放"
				if source == sourceHongguo {
					wantViews = "0"
				}
				if row.ID == fresh.ID && (row.OnlineDate != "2026-03-16" || row.Views != wantViews || row.Heat != fresh.Heat || row.Title != "列表标题" || row.SortMetadata == nil || row.SortMetadata.Version != sortMetadataVersion || row.SortMetadata.Pending) {
					t.Errorf("asynchronous metadata was not merged correctly: %+v", row)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("duplicate detail requests: %d", calls.Load())
			}
		})
	}
}

func TestResumeOnlyPreviouslyQueuedMetadata(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.URL.Path != "/detail" || request.URL.Query().Get("series_id") != "7000000000000000002" {
			t.Errorf("unexpected resource: %s", request.URL.Path)
		}
		return rankingHTTPResponse(request, 200, sortDetailFixture("7000000000000000002", "0")), nil
	})
	cache := libraryCache{Dramas: []Drama{
		{ID: "hongguo:7000000000000000001", Source: sourceHongguo, Title: "历史条目"},
		{ID: "hongguo:7000000000000000002", Source: sourceHongguo, Title: "新条目", Heat: "0", Views: "0", Cover: coverAddressFixture, SortMetadata: &sortMetadataState{Pending: true}},
	}}
	if err := writeLibraryCache(d.cfg.dataDirectory(), cache); err != nil {
		t.Fatal(err)
	}
	app := &UIApp{downloader: d, cfg: d.cfg}
	t.Cleanup(app.stopSortMetadata)
	app.loadLibrary()
	waitSortMetadataQueue(t, app)
	if calls.Load() != 1 {
		t.Fatalf("resume requested %d resources, want one pending detail", calls.Load())
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.dramas[0].SortMetadata != nil || app.dramas[1].OnlineDate != "" || app.dramas[1].Views != "0" || app.dramas[1].SortMetadata.Version != sortMetadataVersion {
		t.Fatal("resume changed history, lost zero, or invented an empty source date")
	}
}

func TestPrimarySortFieldsNeedNoAdditionalInterfacesWhenComplete(t *testing.T) {
	hongguo := hongguoDramaFromAny(map[string]any{"series_id": "7615465407347952664", "series_title": "列表剧", "series_cover": coverAddressFixture, "first_visible_time": "1773662280", "hot_score": "20019", "series_play_cnt": "0"}, "测试")
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Errorf("complete primary data triggered an optional request: %s", request.URL.Path)
		return rankingHTTPResponse(request, 404, ""), nil
	})
	for _, drama := range []Drama{hongguo} {
		if drama.OnlineDate != "2026-03-16" || drama.Views != "0" || !hasCompleteSortMetadata(drama) {
			t.Errorf("primary sort fields were lost: %+v", drama)
		}
		if _, err := d.fetchDramaSortMetadata(context.Background(), drama); err != nil {
			t.Fatal(err)
		}
	}
	if hasSortMetric("未知") || hasSortMetric("-1") {
		t.Fatal("unknown metrics were misclassified as available sorting data")
	}
}

func TestBackgroundRequestsYieldToForeground(t *testing.T) {
	limiter := newRequestLimiter(1, time.Nanosecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request := httptest.NewRequest("GET", "https://hongguoduanju.com/search/test", nil)
	release, err := limiter.acquire(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	background := make(chan func(), 1)
	foreground := make(chan func(), 1)
	go func() {
		unlock, _ := limiter.acquire(context.WithValue(ctx, backgroundCatalogKey{}, true), request)
		background <- unlock
	}()
	go func() {
		unlock, _ := limiter.acquire(ctx, request)
		foreground <- unlock
	}()

	for {
		limiter.mu.Lock()
		waiting := limiter.foregroundWaiting
		limiter.mu.Unlock()
		if waiting > 0 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("foreground waiter did not register")
		}
		time.Sleep(time.Millisecond)
	}
	release()
	select {
	case unlock := <-foreground:
		if unlock == nil {
			t.Fatal("foreground timed out")
		}
		select {
		case <-background:
			t.Fatal("background ran alongside a waiting foreground request")
		default:
		}
		unlock()
	case <-background:
		t.Fatal("optional metadata overtook foreground work")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case unlock := <-background:
		if unlock == nil {
			t.Fatal("background did not resume")
		}
		unlock()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestOptionalDetailPermissionFailureDoesNotBlockForeground(t *testing.T) {
	limiter := newRequestLimiter(1, time.Nanosecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	foreground := httptest.NewRequest("GET", "https://hongguoduanju.com/search/test", nil).WithContext(ctx)
	background := foreground.WithContext(context.WithValue(ctx, backgroundCatalogKey{}, true))
	limiter.observe(background, &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}})
	if release, err := limiter.acquire(background.Context(), background); err == nil {
		release()
		t.Fatal("repeated forbidden details must cool down")
	}
	release, err := limiter.acquire(ctx, foreground)
	if err != nil {
		t.Fatal("an optional detail's permissions blocked public search", err)
	}
	release()
	limiter.observe(background, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"60"}}})
	if release, err := limiter.acquire(ctx, foreground); err == nil {
		release()
		t.Fatal("site rate limits must still be respected")
	}
}
