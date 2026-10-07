package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

type embyPosterState struct {
	LocalFile      string    `json:"localFile,omitempty"`
	LocalSource    string    `json:"localSource,omitempty"`
	LocalError     string    `json:"localError,omitempty"`
	LocalCheckedAt time.Time `json:"localCheckedAt"`
	LocalRetryAt   time.Time `json:"localRetryAt"`
	SourceURL      string    `json:"sourceUrl,omitempty"`
	Destination    string    `json:"destination,omitempty"`
	ItemID         string    `json:"itemId,omitempty"`
	ImageTag       string    `json:"imageTag,omitempty"`
	SyncedSource   string    `json:"syncedSource,omitempty"`
	Status         string    `json:"status,omitempty"`
	Error          string    `json:"error,omitempty"`
	CheckedAt      time.Time `json:"checkedAt"`
	SyncedAt       time.Time `json:"syncedAt"`
	RetryAt        time.Time `json:"retryAt"`
	Failures       int       `json:"failures,omitempty"`
}

func embyPosterDestination(settings embySyncSettings, key []byte) string {
	digest := sha256.Sum256([]byte(settings.ServerURL + "\x00" + settings.OutputDir + "\x00" + embyMetadataID(key, "instance")))
	return hex.EncodeToString(digest[:])
}

func embyPosterSourceHash(remote string) string {
	digest := sha256.Sum256([]byte(remote))
	return hex.EncodeToString(digest[:])
}

func embyRemotePosterDue(poster embyPosterState, interval time.Duration) time.Time {
	if poster.Status == "missing" || poster.Status == "unconfigured" || poster.Status == "local" || poster.SourceURL == "" {
		return time.Time{}
	}
	due := poster.CheckedAt.Add(interval)
	if !poster.RetryAt.IsZero() {
		due = poster.RetryAt
	}
	if poster.CheckedAt.IsZero() {
		due = time.Now()
	}
	return due
}

func embyLocalPosterDue(poster embyPosterState, interval time.Duration) time.Time {
	if poster.SourceURL == "" {
		return time.Time{}
	}
	if !poster.LocalRetryAt.IsZero() {
		return poster.LocalRetryAt
	}
	if poster.LocalCheckedAt.IsZero() || poster.LocalFile == "" || poster.LocalSource != embyPosterSourceHash(poster.SourceURL) {
		return time.Now()
	}
	return poster.LocalCheckedAt.Add(interval)
}

func embyPosterDue(poster embyPosterState, interval time.Duration) time.Time {
	due := embyRemotePosterDue(poster, interval)
	if local := embyLocalPosterDue(poster, interval); !local.IsZero() && (due.IsZero() || local.Before(due)) {
		due = local
	}
	return due
}

func (manager *embySyncManager) preparePosters(document *embySyncDocument, targets []Drama, key []byte) {
	destination := embyPosterDestination(document.Settings, key)
	for _, drama := range targets {
		entry := document.Entries[drama.ID]
		if entry.Episodes == 0 && !entry.Merged {
			continue
		}
		poster := entry.Poster
		if poster.Destination != destination {
			poster = embyPosterState{SourceURL: poster.SourceURL, Destination: destination}
		}
		remote := embyDramaCover(drama)
		if remote == "" {
			remote = embyDramaCover(Drama{CoverURL: poster.SourceURL})
		}
		if poster.SourceURL != remote {
			poster.SourceURL = remote
			poster.CheckedAt, poster.RetryAt, poster.Failures = time.Time{}, time.Time{}, 0
			poster.Status, poster.Error = "pending", ""
			poster.LocalCheckedAt, poster.LocalRetryAt = time.Time{}, time.Time{}
		}
		if remote == "" {
			poster.Status, poster.Error = "missing", ""
		} else if document.Settings.ServerURL == "" {
			if poster.LocalFile != "" && poster.LocalSource == embyPosterSourceHash(remote) && poster.LocalError == "" {
				poster.Status, poster.Error = "local", ""
			} else if poster.LocalError != "" {
				poster.Status, poster.Error = "failed", poster.LocalError
			} else {
				poster.Status, poster.Error = "pending", ""
			}
		} else if poster.Status == "" || poster.Status == "missing" || poster.Status == "unconfigured" {
			poster.Status = "pending"
			poster.CheckedAt, poster.RetryAt = time.Time{}, time.Time{}
		}
		entry.Poster = poster
		document.Entries[drama.ID] = entry
	}
}

func (manager *embySyncManager) postponePoster(poster *embyPosterState, waiting bool, err error, key string) {
	poster.Status = "failed"
	if waiting {
		poster.Status = "waiting"
	}
	poster.Error = manager.safeError(err, key)
	poster.CheckedAt = time.Now()
	if poster.Failures < 5 {
		poster.Failures++
	}
	delays := [...]time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute}
	poster.RetryAt = time.Now().Add(delays[poster.Failures-1])
}

func (manager *embySyncManager) synchronizePosters(ctx context.Context, document *embySyncDocument, targets []Drama, key []byte) {
	if document.Settings.ServerURL == "" {
		return
	}
	interval := time.Duration(document.Settings.IntervalMinutes) * time.Minute
	var due []string
	for _, drama := range targets {
		poster := document.Entries[drama.ID].Poster
		if next := embyRemotePosterDue(poster, interval); !next.IsZero() && !next.After(time.Now()) {
			due = append(due, drama.ID)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		left, right := document.Entries[due[i]].Poster.CheckedAt, document.Entries[due[j]].Poster.CheckedAt
		if left.Equal(right) {
			return due[i] < due[j]
		}
		return left.Before(right)
	})
	if len(due) > 20 {
		due = due[:20]
	}
	if len(due) == 0 {
		return
	}
	client := newEmbyAPIClient(document.Settings)
	defer client.close()
	listCtx, cancel := context.WithTimeout(ctx, time.Minute)
	items, listErr := client.series(listCtx)
	cancel()
	for _, id := range due {
		if ctx.Err() != nil {
			break
		}
		entry := document.Entries[id]
		poster := entry.Poster
		document.PosterChecked++
		if listErr != nil {
			manager.postponePoster(&poster, false, listErr, document.Settings.APIKey)
		} else {
			item, err := matchEmbySeries(items, entry.Folder, embyMetadataID(key, id))
			if err != nil {
				manager.postponePoster(&poster, false, err, document.Settings.APIKey)
			} else if item.ID == "" {
				manager.postponePoster(&poster, true, errors.New("等待 Emby 扫描此剧；请确认输出目录已加入电视剧媒体库，并启用 NFO 元数据读取"), document.Settings.APIKey)
			} else {
				manager.synchronizePoster(ctx, client, document, id, &poster, item, key)
			}
		}
		entry.Poster = poster
		document.Entries[id] = entry
	}
}

func (manager *embySyncManager) synchronizePoster(ctx context.Context, client *embyAPIClient, document *embySyncDocument, id string, poster *embyPosterState, item embyMediaItem, key []byte) {
	currentTag := item.ImageTags["Primary"]
	sourceHash := embyPosterSourceHash(poster.SourceURL)
	sameItem := poster.ItemID == item.ID
	managed := sameItem && poster.ImageTag != "" && currentTag == poster.ImageTag
	if currentTag != "" && !managed {
		poster.Status, poster.Error = "existing", ""
		poster.ItemID, poster.ImageTag, poster.SyncedSource = item.ID, "", ""
		poster.SyncedAt = time.Time{}
		poster.CheckedAt, poster.RetryAt, poster.Failures = time.Now(), time.Time{}, 0
		return
	}
	if currentTag != "" && managed && poster.SyncedSource == sourceHash {
		poster.Status, poster.Error, poster.ImageTag = "synced", "", currentTag
		poster.CheckedAt, poster.RetryAt, poster.Failures = time.Now(), time.Time{}, 0
		return
	}
	address := embyCoverURL(Drama{ID: id, CoverURL: poster.SourceURL}, document.Settings.BaseURL, key, document.OwnerID)
	if address == "" {
		manager.postponePoster(poster, false, errors.New("海报地址无效，请更新剧集资料后重试"), document.Settings.APIKey)
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, 95*time.Second)
	defer cancel()
	tag, uploaded, err := client.downloadPoster(requestCtx, item.ID, address)
	if uploaded {
		poster.ItemID, poster.SyncedSource, poster.ImageTag = item.ID, sourceHash, ""
		poster.SyncedAt = time.Now()
		document.PostersUpdated++
	}
	if err != nil {
		manager.postponePoster(poster, false, err, document.Settings.APIKey)
		return
	}
	poster.ImageTag, poster.Status, poster.Error = tag, "synced", ""
	poster.CheckedAt, poster.RetryAt, poster.Failures = time.Now(), time.Time{}, 0
}

func (document *embySyncDocument) countPosters(targets []Drama) {
	document.PostersPending, document.PostersFailed, document.PostersSynced = 0, 0, 0
	for _, drama := range targets {
		poster := document.Entries[drama.ID].Poster
		if poster.LocalError != "" {
			document.PostersFailed++
			continue
		}
		switch poster.Status {
		case "pending", "waiting":
			document.PostersPending++
		case "failed":
			document.PostersFailed++
		case "synced", "local":
			document.PostersSynced++
		}
	}
}

func (document *embySyncDocument) posterError() error {
	if document.PostersFailed == 0 {
		return nil
	}
	return fmt.Errorf("%d 部海报同步失败，分集文件保留，海报会自动重试；请查看下方详情", document.PostersFailed)
}
