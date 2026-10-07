package app

import (
	"context"
	"errors"
	"net/http"
)

func (d *Downloader) fetchDramaCoverAddress(ctx context.Context, drama Drama) (string, error) {
	drama, valid := normalizeHongguoDrama(drama)
	if !valid {
		return "", errors.New("无效的红果剧集 ID")
	}
	result, err := d.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{"series_id": drama.SourceID})
	if err != nil {
		return "", err
	}
	row := nestedMap(result, "data", "video_data")
	if mapString(row, "series_id_str", "series_id") != drama.SourceID {
		return "", errors.New("红果详情返回了其他剧集")
	}
	return hongguoCoverAddress(mapString(row, "series_cover", "cover")), nil
}
