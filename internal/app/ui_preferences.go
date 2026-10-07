package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxPreferenceBytes      = 64 << 10
	maxPreferenceEntries    = 128
	maxPreferenceKeyBytes   = 80
	maxPreferenceValueBytes = 16 << 10
)

var errPreferenceStore = errors.New("偏好数据无法读取，原文件已保留，请检查数据目录或恢复备份")

type viewerPreferenceStore struct {
	mu     sync.Mutex
	path   string
	values map[string]json.RawMessage
	err    error
}

type preferenceUpdate struct {
	Values map[string]json.RawMessage `json:"values"`
}

func (viewer *viewerRecords) preferenceStore() *viewerPreferenceStore {
	viewer.preferencesOnce.Do(func() {
		store := &viewerPreferenceStore{
			path:   filepath.Join(viewer.directory, "preferences.json"),
			values: map[string]json.RawMessage{},
		}
		store.err = store.load()
		viewer.preferences = store
	})
	return viewer.preferences
}

func (store *viewerPreferenceStore) load() error {
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errPreferenceStore
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxPreferenceBytes+1))
	var values map[string]json.RawMessage
	if decoder.Decode(&values) != nil || decoder.Decode(new(any)) != io.EOF || values == nil {
		return errPreferenceStore
	}
	if err := validatePreferenceValues(values); err != nil {
		return errPreferenceStore
	}
	store.values = clonePreferenceValues(values)
	return nil
}

func validatePreferenceKey(key string) error {
	if key == "" || len(key) > maxPreferenceKeyBytes || strings.TrimSpace(key) != key {
		return errors.New("偏好名称无效")
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return errors.New("偏好名称无效")
		}
	}
	return nil
}

func validatePreferenceValues(values map[string]json.RawMessage) error {
	if len(values) > maxPreferenceEntries {
		return errors.New("偏好条目过多")
	}
	total := 2
	for key, value := range values {
		if err := validatePreferenceKey(key); err != nil {
			return err
		}
		if len(value) == 0 || len(value) > maxPreferenceValueBytes || !json.Valid(value) {
			return errors.New("偏好值无效")
		}
		total += len(key) + len(value) + 6
		if total > maxPreferenceBytes {
			return errors.New("偏好数据过大")
		}
	}
	return nil
}

func clonePreferenceValues(values map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

func (store *viewerPreferenceStore) snapshot() map[string]json.RawMessage {
	store.mu.Lock()
	defer store.mu.Unlock()
	return clonePreferenceValues(store.values)
}

func (store *viewerPreferenceStore) update(values map[string]json.RawMessage) error {
	if err := validatePreferenceValues(values); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return store.err
	}
	next := clonePreferenceValues(store.values)
	for key, value := range values {
		next[key] = append(json.RawMessage(nil), value...)
	}
	if err := validatePreferenceValues(next); err != nil {
		return err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if len(data) > maxPreferenceBytes {
		return errors.New("偏好数据过大")
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(store.path), ".preferences-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, store.path); err != nil {
		return err
	}
	store.values = next
	return nil
}

func readPreferenceUpdate(writer http.ResponseWriter, request *http.Request) (map[string]json.RawMessage, bool) {
	if !playbackRequestAllowed(writer, request, http.MethodPost) {
		return nil, false
	}
	if request.Header.Get("X-Juku-Viewer") == "" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请从剧库页面保存偏好"})
		return nil, false
	}
	if contentType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "偏好请求必须使用 JSON"})
		return nil, false
	}
	var input preferenceUpdate
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxPreferenceBytes+1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Values == nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "偏好请求无效"})
		return nil, false
	}
	if err := validatePreferenceValues(input.Values); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return nil, false
	}
	return input.Values, true
}

func (app *UIApp) handlePreferences(writer http.ResponseWriter, request *http.Request) {
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	store := viewer.preferenceStore()
	if store.err != nil {
		writeViewerError(writer, http.StatusServiceUnavailable, "preferences_unavailable", publicError(store.err).Error())
		return
	}
	switch request.Method {
	case http.MethodGet:
		if !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"values": store.snapshot()})
	case http.MethodPost:
		values, ok := readPreferenceUpdate(writer, request)
		if !ok {
			return
		}
		if err := store.update(values); err != nil {
			writeViewerError(writer, http.StatusInternalServerError, "preferences_save_failed", "偏好保存失败："+publicError(err).Error())
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"values": store.snapshot()})
	default:
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
	}
}
