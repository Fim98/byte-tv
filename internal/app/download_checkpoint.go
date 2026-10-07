package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type downloadCheckpointState struct {
	URL       string `json:"url"`
	Validator string `json:"validator,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Complete  bool   `json:"complete,omitempty"`
}

type downloadCheckpointRequest func(context.Context, int64, string) (*http.Request, error)
type downloadCheckpointFetch func(*http.Request) (*http.Response, error)

type downloadMediaCache struct {
	directory string
}

func downloadHTTPValidator(header http.Header) string {
	if value := strings.TrimSpace(header.Get("ETag")); value != "" && !strings.HasPrefix(value, "W/") {
		return value
	}
	if value := strings.TrimSpace(header.Get("Last-Modified")); value != "" {
		if _, err := http.ParseTime(value); err == nil {
			return value
		}
	}
	return ""
}

func readDownloadCheckpoint(path, address string) downloadCheckpointState {
	var state downloadCheckpointState
	body, err := os.ReadFile(path)
	if err != nil || len(body) > 64<<10 || json.Unmarshal(body, &state) != nil || state.URL != address {
		return downloadCheckpointState{}
	}
	return state
}

func writeDownloadCheckpoint(path string, state downloadCheckpointState) error {
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".download-checkpoint-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(body); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func checkpointExistingComplete(path string, state downloadCheckpointState, key bool) (int64, bool) {
	if !state.Complete {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || state.Total > 0 && info.Size() != state.Total || key && info.Size() != 16 {
		return 0, false
	}
	return info.Size(), true
}

func checkpointPartialOffset(path string, state downloadCheckpointState, key bool) int64 {
	if state.Validator == "" || key {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || state.Total > 0 && info.Size() > state.Total {
		return 0
	}
	return info.Size()
}

func runDownloadCheckpoint(ctx context.Context, address, target, statePath string, key bool, build downloadCheckpointRequest, fetch downloadCheckpointFetch, progress func(int64, int64)) (int64, error) {
	if !isProviderHTTPMediaURL(address) {
		return 0, errors.New("媒体下载地址无效")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	partial := target + ".part"
	state := readDownloadCheckpoint(statePath, address)
	if size, ok := checkpointExistingComplete(target, state, key); ok {
		if progress != nil {
			progress(size, size)
		}
		return size, nil
	}
	_ = os.Remove(target)
	offset := checkpointPartialOffset(partial, state, key)
	for restart := 0; restart < 2; restart++ {
		size, reset, err := runDownloadCheckpointAttempt(ctx, address, target, partial, statePath, key, offset, state, build, fetch, progress)
		if !reset {
			return size, err
		}
		offset, state = 0, downloadCheckpointState{}
		_ = os.Remove(partial)
		_ = os.Remove(statePath)
	}
	return 0, errors.New("服务器无法恢复下载，请重试")
}

func runDownloadCheckpointAttempt(ctx context.Context, address, target, partial, statePath string, key bool, offset int64, old downloadCheckpointState, build downloadCheckpointRequest, fetch downloadCheckpointFetch, progress func(int64, int64)) (int64, bool, error) {
	request, err := build(ctx, offset, old.Validator)
	if err != nil {
		return 0, false, err
	}
	response, err := fetch(request)
	if err != nil {
		return offset, false, err
	}
	defer response.Body.Close()
	if offset > 0 && (response.StatusCode == http.StatusRequestedRangeNotSatisfiable || response.StatusCode == http.StatusPreconditionFailed) {
		return 0, true, nil
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return offset, false, fmt.Errorf("媒体下载失败 HTTP %d", response.StatusCode)
	}
	if response.StatusCode == http.StatusOK {
		offset = 0
	}
	if !identityMediaResponse(response) {
		return offset, false, errors.New("服务器返回了压缩或分段媒体响应，无法安全续传")
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if !key && (strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/json")) {
		return offset, false, errors.New("站源返回了错误页面，请刷新下载地址后重试")
	}
	total := response.ContentLength
	validator := downloadHTTPValidator(response.Header)
	if response.StatusCode == http.StatusPartialContent {
		span, valid := parseMediaByteRange(response.Header.Get("Content-Range"))
		if !valid || span.start != offset || response.ContentLength >= 0 && response.ContentLength != span.end-span.start+1 {
			return offset, false, errors.New("服务器返回的续传范围不正确，已停止下载")
		}
		if offset > 0 && (validator != "" && validator != old.Validator || old.Total > 0 && old.Total != span.total) {
			return 0, true, nil
		}
		total = span.total
	}
	if key && (total > 64 || offset > 0) {
		return offset, false, errors.New("视频密钥格式无效")
	}
	state := downloadCheckpointState{URL: address, Validator: validator, Total: total}
	if err := writeDownloadCheckpoint(statePath, state); err != nil {
		return offset, false, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	file, err := os.OpenFile(partial, flags, 0o600)
	if err != nil {
		return offset, false, err
	}
	received := offset
	buffer := make([]byte, 64<<10)
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if key && received+int64(count) > 64 {
				err = errors.New("视频密钥过大")
				break
			}
			written, writeErr := file.Write(buffer[:count])
			received += int64(written)
			if writeErr != nil || written != count {
				err = errors.New("写入下载文件失败，请检查剩余空间")
				break
			}
			if progress != nil {
				progress(received, total)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				err = errors.New("媒体文件未接收完整，点击继续下载")
			}
			break
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
			break
		}
	}
	if err == nil && (received == 0 || total >= 0 && received != total) {
		err = errors.New("媒体文件未接收完整，点击继续下载")
	}
	if err == nil && key && received != 16 {
		err = errors.New("视频密钥长度无效")
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return received, false, err
	}
	if closeErr != nil {
		return received, false, closeErr
	}
	if err = os.Rename(partial, target); err != nil {
		return received, false, err
	}
	state.Complete, state.Total = true, received
	if err = writeDownloadCheckpoint(statePath, state); err != nil {
		return received, false, err
	}
	return received, false, nil
}

func directDownloadMedia(media providerMedia, key []byte) bool {
	if media.Playlist != "" || len(key) > 0 || len(media.HLSKey) > 0 || len(media.CENCKey) > 0 || !validProviderMediaCredentials(media.credentials, providerMediaCredentialReserve) {
		return false
	}
	parsed, err := url.Parse(media.URL)
	return err == nil && strings.HasSuffix(strings.ToLower(parsed.Path), ".mp4")
}

func (d *Downloader) downloadDirectProviderMedia(ctx context.Context, task Task, media providerMedia, key []byte, progress *downloadProgressState) (bool, error) {
	if !directDownloadMedia(media, key) {
		return false, nil
	}
	candidates := append([]providerMedia{media}, mediaFallbackVariants(media)...)
	client := *d.client
	client.Timeout = 0
	var lastErr error
	for _, candidate := range candidates {
		if !directDownloadMedia(candidate, key) {
			continue
		}
		build := func(ctx context.Context, offset int64, validator string) (*http.Request, error) {
			request, err := http.NewRequestWithContext(providerMediaContext(ctx, candidate.credentials), http.MethodGet, candidate.URL, nil)
			if err != nil {
				return nil, err
			}
			request.Header.Set("Referer", candidate.Referer)
			request.Header.Set("Accept-Encoding", "identity")
			if offset > 0 {
				request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
				request.Header.Set("If-Range", validator)
			}
			return request, nil
		}
		fetch := func(request *http.Request) (*http.Response, error) {
			return d.doMediaRequestWithClient(request, &client)
		}
		size, err := runDownloadCheckpoint(ctx, candidate.URL, task.OutPath, task.OutPath+".download.json", false, build, fetch, func(received, total int64) {
			if progress == nil {
				return
			}
			progress.setByteProgress(received, total)
			progress.report("downloading", false)
		})
		if err == nil {
			if progress != nil {
				progress.setTotalBytes(size)
				progress.report("completed", true)
			}
			return true, nil
		}
		lastErr = err
	}
	return true, lastErr
}

func prepareDownloadMediaCache(directory string, media providerMedia, key []byte) (*downloadMediaCache, error) {
	fingerprint := sha256.New()
	for _, value := range []string{media.URL, media.Referer, media.Playlist, hex.EncodeToString(media.HLSKey), hex.EncodeToString(media.CENCKey), hex.EncodeToString(key), fmt.Sprint(media.Quality)} {
		_, _ = fingerprint.Write([]byte(value))
		_, _ = fingerprint.Write([]byte{0})
	}
	marker := hex.EncodeToString(fingerprint.Sum(nil))
	markerPath := filepath.Join(directory, ".fingerprint")
	if previous, err := os.ReadFile(markerPath); err == nil && strings.TrimSpace(string(previous)) != marker {
		if err := os.RemoveAll(directory); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := writeNewViewerFile(markerPath, []byte(marker)); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if previous, readErr := os.ReadFile(markerPath); readErr != nil || strings.TrimSpace(string(previous)) != marker {
			return nil, errors.New("下载检查点无法保存")
		}
	}
	return &downloadMediaCache{directory: directory}, nil
}

func (cache *downloadMediaCache) path(address string) (string, string) {
	sum := sha256.Sum256([]byte(address))
	base := filepath.Join(cache.directory, hex.EncodeToString(sum[:]))
	return base + ".bin", base + ".json"
}

func (cache *downloadMediaCache) open(ctx context.Context, proxy *hlsProxy, asset hlsAsset, build func(hlsAsset) (*http.Request, error)) (*os.File, int64, error) {
	target, statePath := cache.path(asset.remote)
	buildRequest := func(ctx context.Context, offset int64, validator string) (*http.Request, error) {
		request, err := build(asset)
		if err != nil {
			return nil, err
		}
		request = request.WithContext(ctx)
		request.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			request.Header.Set("If-Range", validator)
		}
		return request, nil
	}
	size, err := runDownloadCheckpoint(ctx, asset.remote, target, statePath, asset.extension == ".key", buildRequest, proxy.fetch, nil)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(target)
	return file, size, err
}
