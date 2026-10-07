package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type embyAPIClient struct {
	settings  embySyncSettings
	client    *http.Client
	transport *http.Transport
}

type embyMediaItem struct {
	ID          string            `json:"Id"`
	Type        string            `json:"Type"`
	Path        string            `json:"Path"`
	ProviderIDs map[string]string `json:"ProviderIds"`
	ImageTags   map[string]string `json:"ImageTags"`
}

type embyItemList struct {
	Items            []embyMediaItem `json:"Items"`
	TotalRecordCount int             `json:"TotalRecordCount"`
}

func newEmbyAPIClient(settings embySyncSettings) *embyAPIClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &embyAPIClient{settings: settings, transport: transport,
		client: &http.Client{Transport: transport, Timeout: 95 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (client *embyAPIClient) close() {
	client.transport.CloseIdleConnections()
}

func (client *embyAPIClient) request(ctx context.Context, method, endpoint string, query url.Values, result any) error {
	address := client.settings.ServerURL + endpoint
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, address, nil)
	if err != nil {
		return errors.New("Emby 请求地址无效")
	}
	request.Header.Set("X-Emby-Token", client.settings.APIKey)
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost && strings.HasSuffix(endpoint, "/RemoteImages/Download") {
		request.Body = io.NopCloser(strings.NewReader("{}"))
		request.ContentLength = 2
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.client.Do(request)
	if err != nil {
		var failure *url.Error
		if errors.As(err, &failure) {
			err = failure.Err
		}
		return fmt.Errorf("Emby 连接失败：%w", publicError(err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Emby HTTP %d", response.StatusCode)
	}
	if result == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return err
	}
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return errors.New("Emby 响应读取失败")
	}
	if len(body) > limit || json.Unmarshal(body, result) != nil {
		return errors.New("Emby 返回的剧集信息无效或过大")
	}
	return nil
}

func (client *embyAPIClient) series(ctx context.Context) ([]embyMediaItem, error) {
	var items []embyMediaItem
	seen := make(map[string]bool)
	for start := 0; start < 100000; {
		query := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Series"}, "Fields": {"Path,ProviderIds"},
			"EnableImages": {"true"}, "EnableImageTypes": {"Primary"}, "ImageTypeLimit": {"1"},
			"SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "StartIndex": {strconv.Itoa(start)}, "Limit": {"200"}}
		var page embyItemList
		if err := client.request(ctx, http.MethodGet, "/Items", query, &page); err != nil {
			return nil, err
		}
		added := 0
		for _, item := range page.Items {
			if validEmbyItemID(item.ID) && item.Type == "Series" && !seen[item.ID] {
				seen[item.ID] = true
				items = append(items, item)
				added++
			}
		}
		start += len(page.Items)
		if len(page.Items) == 0 || page.TotalRecordCount > 0 && start >= page.TotalRecordCount ||
			page.TotalRecordCount == 0 && len(page.Items) < 200 {
			return items, nil
		}
		if added == 0 {
			return nil, errors.New("Emby 剧集分页未前进，请稍后重试")
		}
	}
	return nil, errors.New("Emby 剧集列表超过 100000 部，暂时无法完成海报匹配")
}

func validEmbyItemID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if char != '-' && char != '_' && (char < '0' || char > '9') && (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			return false
		}
	}
	return true
}

func embyProviderID(item embyMediaItem, provider string) string {
	for name, value := range item.ProviderIDs {
		if strings.EqualFold(name, provider) {
			return value
		}
	}
	return ""
}

func matchEmbySeries(items []embyMediaItem, folder, identity string) (embyMediaItem, error) {
	var matches []embyMediaItem
	for _, item := range items {
		if item.Type != "Series" || !validEmbyItemID(item.ID) {
			continue
		}
		providerID := embyProviderID(item, "juku")
		if providerID != "" && providerID != identity {
			continue
		}
		itemPath := strings.TrimRight(strings.ReplaceAll(item.Path, "\\", "/"), "/")
		if !validEmbyFolder(folder) || folder == "" || !strings.HasSuffix("/"+itemPath, "/"+folder) {
			continue
		}
		matches = append(matches, item)
	}
	if len(matches) > 1 {
		return embyMediaItem{}, errors.New("Emby 中存在多个相同导出目录，请移除重复的媒体库挂载后重试")
	}
	if len(matches) == 0 {
		return embyMediaItem{}, nil
	}
	return matches[0], nil
}

func (client *embyAPIClient) downloadPoster(ctx context.Context, id, address string) (string, bool, error) {
	if !validEmbyItemID(id) {
		return "", false, errors.New("Emby 剧集 ID 无效")
	}
	query := url.Values{"Type": {"Primary"}, "ImageUrl": {address}}
	if err := client.request(ctx, http.MethodPost, "/Items/"+id+"/RemoteImages/Download", query, nil); err != nil {
		return "", false, err
	}
	tag, err := client.posterTag(ctx, id)
	return tag, true, err
}

func (client *embyAPIClient) posterTag(ctx context.Context, id string) (string, error) {
	var result embyItemList
	if err := client.request(ctx, http.MethodGet, "/Items", url.Values{"Ids": {id}, "EnableImages": {"true"}}, &result); err != nil {
		return "", err
	}
	for _, item := range result.Items {
		if item.ID == id && item.ImageTags["Primary"] != "" {
			return item.ImageTags["Primary"], nil
		}
	}
	return "", errors.New("Emby 尚未返回海报状态，将自动重试")
}
