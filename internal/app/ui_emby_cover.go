package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func embyMetadataID(key []byte, dramaID string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("emby-metadata-v1\x00" + dramaID))
	return hex.EncodeToString(mac.Sum(nil))
}

func embyCoverToken(key []byte, dramaID, owner, version string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("emby-cover-v1\x00" + owner + "\x00" + dramaID + "\x00" + version))
	return hex.EncodeToString(mac.Sum(nil))
}

func embyDramaCover(drama Drama) string {
	raw := bestDramaCover(drama)
	if raw == "" || len(raw) > 4096 {
		return ""
	}
	remote, ok := buildImageURL(raw)
	if !ok {
		return ""
	}
	return remote
}

func embyCoverURL(drama Drama, base string, key []byte, owner string) string {
	remote := embyDramaCover(drama)
	if remote == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(remote))
	version := hex.EncodeToString(digest[:16])
	query := url.Values{"id": {drama.ID}, "v": {version}, "key": {embyCoverToken(key, drama.ID, owner, version)}}
	if owner != "" {
		query.Set("account", owner)
	}
	return base + "/api/emby/cover?" + query.Encode()
}

func (app *UIApp) embyCoverSource(id string) string {
	app.mu.Lock()
	for _, drama := range app.dramas {
		if drama.ID == id {
			remote := embyDramaCover(drama)
			if remote != "" {
				app.mu.Unlock()
				return remote
			}
			break
		}
	}
	app.mu.Unlock()
	manager := app.embySyncer()
	manager.mu.Lock()
	remote := manager.document.Entries[id].Poster.SourceURL
	manager.mu.Unlock()
	if remote != "" {
		if normalized, ok := buildImageURL(remote); ok {
			return normalized
		}
	}
	return ""
}

func (app *UIApp) handleEmbyCover(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
		return
	}
	query := request.URL.Query()
	id, owner, version := query.Get("id"), query.Get("account"), query.Get("v")
	canonical, _, valid := playbackHistoryIdentity(id)
	supplied, err := hex.DecodeString(query.Get("key"))
	versionBytes, versionErr := hex.DecodeString(version)
	key, keyErr := app.embySigningKey(false)
	if !valid || id != canonical || len(id) > 256 || len(owner) > 256 || strings.ContainsAny(owner, "\x00\r\n") ||
		err != nil || len(supplied) != 32 || versionErr != nil || len(versionBytes) != 16 || keyErr != nil {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "Emby 海报链接无效，请重新同步或导出"})
		return
	}
	expected, _ := hex.DecodeString(embyCoverToken(key, id, owner, version))
	if !hmac.Equal(expected, supplied) || owner != "" && !app.embyAccountSourceAllowed(owner, id) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "Emby 海报链接无效或账号权限已变更"})
		return
	}
	remote := app.embyCoverSource(id)
	if remote == "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此剧暂无可用海报"})
		return
	}
	source, _, _ := splitProviderDramaID(id)
	ctx, cancel := context.WithTimeout(context.WithValue(request.Context(), coverSourceKey{}, source), 90*time.Second)
	defer cancel()
	data, err := app.loadCoverImage(ctx, remote, nil)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "读取海报失败：" + app.redactError(err)})
		return
	}
	writer.Header().Set("Content-Type", imageContentType(data))
	http.ServeContent(writer, request, "cover", time.Time{}, bytes.NewReader(data))
}
