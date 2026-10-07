package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func parityRankingHTML(board rankingBoard, jsonLD bool) string {
	header := `<link rel="canonical" href="https://www.hongguoduanju.com/rank/` + board.path + `"><nav aria-label="榜单分页"><span aria-current="page">1</span></nav>`
	if jsonLD {
		return header + `<script type="application/ld+json">{"@type":"ItemList","url":"/rank/` + board.path + `","itemListElement":[{"position":1,"name":"合成榜单剧","url":"/detail?series_id=700001"}]}</script>`
	}
	return header + `<ol aria-label="热播榜"><li><article aria-labelledby="rank-title-700001"><a aria-label="查看短剧" href="/detail?series_id=700001">1</a><a href="/detail?series_id=700001"><h2 id="rank-title-700001">合成榜单剧</h2></a><span>12.3万热度</span></article></li></ol>`
}

func TestParityHongguoRankingTextFallbackAndIdentity(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	for _, jsonLD := range []bool{false, true} {
		body := parityRankingHTML(board, jsonLD)
		page, err := parseHongguoRanking(body, board, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].Drama.ID != "hongguo:700001" || page.Items[0].Rank != 1 || page.HasMore {
			t.Fatalf("text fallback failed (JSON-LD=%v): %+v %v", jsonLD, page, err)
		}
		if _, err := parseHongguoRanking(strings.ReplaceAll(body, board.path, "hot-ai-drama"), board, 1); err == nil {
			t.Fatal("a different ranking board was accepted")
		}
		if _, err := parseHongguoRanking(body, board, 2); err == nil {
			t.Fatal("a different page was accepted")
		}
		if _, err := parseHongguoRanking(strings.ReplaceAll(body, "700001", "invalid"), board, 1); err == nil {
			t.Fatal("invalid drama identity was accepted")
		}
	}
}

func TestParityHongguoRankingRetryIsBoundedAndBypassesCache(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		count := calls.Add(1)
		if request.URL.RawQuery != "" {
			t.Error("first page is not canonical")
		}
		if count > 1 && (request.Header.Get("Cache-Control") != "no-cache" || request.Header.Get("Pragma") != "no-cache") {
			t.Error("retry reused the incomplete cached page")
		}
		return rankingHTTPResponse(request, 200, `<link rel="canonical" href="/rank/hot-drama">`), nil
	})
	d.cfg.Retries = 10
	_, err := d.fetchHongguoRankingPage(context.Background(), board, 1)
	if !errors.Is(err, errHongguoRankingIncomplete) || calls.Load() != 3 {
		t.Fatalf("retry budget changed: %d %v", calls.Load(), err)
	}
}

func TestParityHongguoSearchDeadlineKeepsSharedPartialResult(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("unexpected network request: %s", request.URL.Path)
	})
	pending := &hongguoSearchCall{done: make(chan struct{}), changed: make(chan struct{}), snapshot: hongguoSearchEntry{Dramas: []Drama{{ID: "hongguo:700001", Title: "合成结果"}}, Total: 1}}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	result, err := d.waitHongguoSearch(ctx, pending, nil)
	if err != nil || len(result.Dramas) != 1 || !result.Limited || result.Warning == "" {
		t.Fatalf("deadline discarded partial result: %+v %v", result, err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := d.waitHongguoSearch(canceled, pending, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("explicit cancellation became a successful search")
	}
}

func TestParityHongguoSearchSortKeepsFamiliesAndTokenMatches(t *testing.T) {
	if hongguoTitleSearchRank("合成 第五季 故事", "合成 故事") >= hongguoTitleSearchRank("无关内容", "合成 故事") {
		t.Fatal("separate title terms did not outrank unrelated results")
	}
	rows := []Drama{{Title: "合成故事第十季"}, {Title: "合成番外第二季"}, {Title: "合成故事第二季"}, {Title: "合成故事第一季"}}
	sortHongguoSearchDramas(rows, "合成")
	want := []string{"合成故事第一季", "合成番外第二季", "合成故事第二季", "合成故事第十季"}
	for index, drama := range rows {
		if drama.Title != want[index] {
			t.Fatalf("cross-family order changed: %+v", rows)
		}
	}
}

func TestParityHongguoMediaRetainsBackupsWithoutInventingPrimary(t *testing.T) {
	primary := "https://media.example.test/main.mp4?token=a%2Fb"
	backup := "https://backup.example.test/backup.mp4"
	model := map[string]any{"video_list": []any{map[string]any{
		"main_url": "invalid", "backup_url": base64.StdEncoding.EncodeToString([]byte(primary)), "url_list": []any{primary, backup},
		"video_meta": map[string]any{"codec_type": "h264", "vheight": "720"},
	}}}
	media, err := selectHongguoAppMedia(model)
	if err != nil || media.URL != primary || len(media.Variants) != 2 || media.Variants[1].URL != backup || media.Quality != 720 {
		t.Fatalf("backup media fields were lost: %+v %v", media, err)
	}
}
