package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var sortMetadataMetric = regexp.MustCompile(`(?i)^\d+(?:\.\d+)?(?:亿|万|千|w|k|m|b)?\+?(?:次播放|次观看|人看过|热度|播放|观看|次)?\+?$`)

func hasSortMetric(value string) bool {
	return sortMetadataMetric.MatchString(strings.NewReplacer(",", "", "，", "", " ", "", "\t", "").Replace(value))
}

func hasCompleteSortMetadata(drama Drama) bool {
	if providerReleaseDate(drama.OnlineDate) == "" || !hasSortMetric(drama.Views) {
		return false
	}
	switch dramaProvider(drama) {
	case sourceHongguo:
		return hasSortMetric(drama.Heat)
	}
	return false
}

func providerReleaseDate(value string) string {
	value = strings.TrimSpace(value)
	if stamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
		value = stamp.In(providerChinaTime).Format("2006-01-02")
	}
	date := normalizeDate(value)
	if parsed, err := time.Parse("2006-01-02", date); err != nil || parsed.Year() < 2000 || parsed.Year() > 2100 {
		return ""
	}
	return date
}

func providerTimestampDate(value string) string {
	stamp, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || stamp <= 0 {
		return ""
	}
	if stamp > 100_000_000_000 {
		stamp /= 1000
	}
	date := time.Unix(stamp, 0).In(providerChinaTime)
	if date.Year() < 2000 || date.Year() > 2100 {
		return ""
	}
	return date.Format("2006-01-02")
}

func (d *Downloader) fetchDramaSortMetadata(ctx context.Context, drama Drama) (Drama, error) {
	source, id, ok := splitProviderDramaID(drama.ID)
	patch := Drama{ID: drama.ID, Source: source, SourceID: id}
	if !ok {
		return patch, errors.New("无效的剧集 ID")
	}
	needCover := needsHongguoCoverAddress(drama)
	if hasCompleteSortMetadata(drama) && !needCover {
		return patch, nil
	}
	switch source {
	case sourceHongguo:
		if !hongguoNumericID.MatchString(id) {
			return patch, errors.New("无效的红果剧集 ID")
		}
		var failures []error
		if providerReleaseDate(drama.OnlineDate) == "" {
			body, err := d.fetchProviderText(ctx, hongguoBaseURL+"/detail?series_id="+url.QueryEscape(id), hongguoBaseURL+"/")
			if err == nil {
				var web Drama
				web, err = parseHongguoSortDetail(body, id)
				patch = mergeDramaMetadata(web, patch)
			}
			if err != nil {
				failures = append(failures, err)
			}
		}
		needMetrics := !hasSortMetric(drama.Heat) || !hasSortMetric(drama.Views)
		if !needMetrics && (!needCover || bestDramaCover(patch) != "") {
			return patch, errors.Join(failures...)
		}
		result, appErr := d.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{"series_id": id})
		if appErr == nil {
			row := nestedMap(result, "data", "video_data")
			if mapString(row, "series_id_str", "series_id") != id {
				appErr = errors.New("红果详情返回了其他剧集")
			} else {
				patch.Title = mapString(row, "series_title", "series_name")
				if needMetrics {
					patch.Heat = hongguoHeat(row)
					patch.Views = normalizeViews(mapString(row, "series_play_cnt", "play_cnt"))
				}
				if needCover {
					if cover := hongguoCoverAddress(mapString(row, "series_cover", "cover")); cover != "" {
						patch.Cover, patch.CoverURL = cover, cover
					}
				}
			}
		}
		if appErr != nil {
			failures = append(failures, appErr)
		}
		return patch, errors.Join(failures...)

	}
	return patch, errors.New("该站源暂无资料补齐接口")
}

func parseHongguoSortDetail(body, id string) (Drama, error) {
	row := nestedMap(routerLoaderMap(parseRouterData(body), "detail_page", "detail_"), "seriesDetail")
	if mapString(row, "series_id", "series_id_str") != id || mapString(row, "series_name", "series_title") == "" {
		return Drama{}, errors.New("红果网页详情与请求剧集不符")
	}
	patch := hongguoDramaFromAny(row, "")
	patch.Title = mapString(row, "series_name", "series_title")
	patch.Name = patch.Title
	patch.OnlineDate = providerTimestampDate(mapString(row, "first_visible_time"))
	if cover := hongguoCoverAddress(mapString(row, "series_cover", "cover")); cover != "" {
		patch.Cover, patch.CoverURL = cover, cover
	}
	return patch, nil
}
