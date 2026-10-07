package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var embyPosterNames = []string{"poster.jpg", "poster.jpeg", "poster.png", "poster.webp", "poster.gif", "folder.jpg", "folder.png"}

func embyArtworkHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func readEmbyPosterDirectory(settings embySyncSettings, drama Drama, folder string) (string, embyFolderManifest, error) {
	var manifest embyFolderManifest
	root, err := filepath.EvalSymlinks(settings.OutputDir)
	if err != nil {
		return "", manifest, err
	}
	if _, err = embyDirectoryParent(root, folder, false); err != nil {
		return "", manifest, err
	}
	directory := filepath.Join(root, filepath.FromSlash(folder))
	info, err := os.Lstat(directory)
	if err != nil {
		return "", manifest, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", manifest, errors.New("Emby 海报目录不是普通文件夹")
	}
	manifestPath := filepath.Join(directory, ".juku-emby.json")
	info, err = os.Lstat(manifestPath)
	if err != nil {
		return "", manifest, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return "", manifest, errors.New("Emby 目录标识不是有效的普通文件")
	}
	if err = readEmbyJSON(manifestPath, &manifest, 8<<20); err != nil {
		return "", manifest, err
	}
	if manifest.Version != 1 || manifest.DramaID != drama.ID {
		return "", manifest, errors.New("Emby 海报目录所属剧集不匹配")
	}
	return directory, manifest, nil
}

func inspectEmbyArtwork(directory string, manifest embyFolderManifest) (managed, preserved string, err error) {
	for _, name := range embyPosterNames {
		path := filepath.Join(directory, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if !info.Mode().IsRegular() {
			return "", "", errors.New("Emby 海报不是普通文件，已保留原内容")
		}
		if name != manifest.PosterFile || manifest.PosterHash == "" || info.Size() > maxCoverBytes {
			return "", name, nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		if embyArtworkHash(body) != manifest.PosterHash {
			return "", name, nil
		}
		managed = name
	}
	return managed, "", nil
}

func (app *UIApp) syncLocalEmbyPoster(ctx context.Context, settings embySyncSettings, drama Drama, folder, remote string) (string, bool, error) {
	directory, manifest, err := readEmbyPosterDirectory(settings, drama, folder)
	if err != nil {
		return "", false, err
	}
	managed, preserved, err := inspectEmbyArtwork(directory, manifest)
	if err != nil || preserved != "" {
		return preserved, false, err
	}
	sourceHash := embyPosterSourceHash(remote)
	if managed != "" && manifest.PosterSource == sourceHash {
		return managed, false, nil
	}
	source := sourceHongguo
	data, err := app.loadCoverImage(context.WithValue(ctx, coverSourceKey{}, source), remote, nil)
	if err != nil {
		return "", false, err
	}
	extension := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}[imageContentType(data)]
	if extension == "" {
		return "", false, errors.New("海报图片格式不受支持")
	}
	if err = ctx.Err(); err != nil {
		return "", false, err
	}

	directory, manifest, err = readEmbyPosterDirectory(settings, drama, folder)
	if err != nil {
		return "", false, err
	}
	managed, preserved, err = inspectEmbyArtwork(directory, manifest)
	if err != nil || preserved != "" {
		return preserved, false, err
	}
	name := "poster" + extension
	changed, err := writeEmbyFile(filepath.Join(directory, name), data, 0644)
	if err != nil {
		return "", false, err
	}
	previousHash := manifest.PosterHash
	manifest.PosterFile, manifest.PosterHash, manifest.PosterSource = name, embyArtworkHash(data), sourceHash
	if err = writeEmbyJSON(filepath.Join(directory, ".juku-emby.json"), manifest, 0600); err != nil {
		return name, changed, err
	}
	if managed != "" && managed != name {
		old := filepath.Join(directory, managed)
		if info, err := os.Lstat(old); err == nil && info.Mode().IsRegular() && info.Size() <= maxCoverBytes {
			if body, err := os.ReadFile(old); err == nil && embyArtworkHash(body) == previousHash {
				_ = os.Remove(old)
			}
		}
	}
	return name, changed, nil
}

func (manager *embySyncManager) synchronizeLocalPosters(ctx context.Context, document *embySyncDocument, targets []Drama) {
	interval := time.Duration(document.Settings.IntervalMinutes) * time.Minute
	var due []Drama
	for _, drama := range targets {
		entry := document.Entries[drama.ID]
		poster := entry.Poster
		if entry.Folder == "" {
			continue
		}
		if next := embyLocalPosterDue(poster, interval); !next.IsZero() && !next.After(time.Now()) {
			due = append(due, drama)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		left, right := document.Entries[due[i].ID].Poster.LocalCheckedAt, document.Entries[due[j].ID].Poster.LocalCheckedAt
		if left.Equal(right) {
			return due[i].ID < due[j].ID
		}
		return left.Before(right)
	})
	if len(due) > 20 {
		due = due[:20]
	}
	for _, drama := range due {
		if ctx.Err() != nil {
			return
		}
		entry := document.Entries[drama.ID]
		poster := entry.Poster
		requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		name, changed, err := manager.app.syncLocalEmbyPoster(requestCtx, document.Settings, drama, entry.Folder, poster.SourceURL)
		cancel()
		document.PosterChecked++
		poster.LocalCheckedAt = time.Now()
		poster.LocalError = manager.safeError(err, document.Settings.APIKey)
		if err != nil {
			poster.LocalRetryAt = time.Now().Add(5 * time.Minute)
		} else {
			poster.LocalFile, poster.LocalSource, poster.LocalRetryAt = name, embyPosterSourceHash(poster.SourceURL), time.Time{}
		}
		if changed {
			document.WrittenFiles++
			document.RefreshPending = document.Settings.ServerURL != ""
		}
		if document.Settings.ServerURL == "" {
			poster.Status, poster.Error, poster.CheckedAt, poster.RetryAt = "local", poster.LocalError, poster.LocalCheckedAt, poster.LocalRetryAt
			if err != nil {
				poster.Status = "failed"
			}
		}
		entry.Poster = poster
		document.Entries[drama.ID] = entry
	}
}
