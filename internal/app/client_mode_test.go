package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clientTestServer(t *testing.T) (*clientServer, http.Handler) {
	t.Helper()
	app := viewerTestApp(t)
	app.clientMode = true
	if err := app.prepareBrowserViewers(); err != nil {
		t.Fatal(err)
	}
	server := &clientServer{ui: app, token: strings.Repeat("a", 48), port: 40123}
	return server, server.handler()
}

func clientRequest(handler http.Handler, method, path, token, host string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1:40123"+path, strings.NewReader("{}"))
	request.Host = host
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("X-Juku-Token", token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestClientModeRequiresTokenAndLoopbackHost(t *testing.T) {
	server, handler := clientTestServer(t)
	if code := clientRequest(handler, "GET", "/api/desktop/status", "", "127.0.0.1:40123", nil, nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("missing token: %d", code)
	}
	if code := clientRequest(handler, "GET", "/api/desktop/status", "wrong", "127.0.0.1:40123", nil, nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", code)
	}
	if code := clientRequest(handler, "GET", "/api/desktop/status", server.token, "evil.example:40123", nil, nil).Code; code != http.StatusForbidden {
		t.Fatalf("rebinding host: %d", code)
	}
	login := clientRequest(handler, "GET", "/?token="+server.token, "", "127.0.0.1:40123", nil, nil)
	if login.Code != http.StatusFound || len(login.Result().Cookies()) == 0 || !login.Result().Cookies()[0].HttpOnly {
		t.Fatalf("token exchange: %d %v", login.Code, login.Result().Cookies())
	}
	if code := clientRequest(handler, "GET", "/api/desktop/status", "", "127.0.0.1:40123", login.Result().Cookies(), nil).Code; code != http.StatusOK {
		t.Fatalf("cookie auth: %d", code)
	}
}

func TestClientOwnerCanUseAdminEndpointsWithoutPassword(t *testing.T) {
	server, handler := clientTestServer(t)
	status := clientRequest(handler, "GET", "/api/desktop/status", server.token, "127.0.0.1:40123", nil, nil)
	var body map[string]any
	_ = json.Unmarshal(status.Body.Bytes(), &body)
	if body["owner"] != true || body["clientMode"] != true {
		t.Fatalf("status: %s", status.Body.String())
	}
	viewer := clientRequest(handler, "GET", "/api/ui/viewer?confirm=1", server.token, "127.0.0.1:40123", nil, nil)
	cookies := viewer.Result().Cookies()
	config := clientRequest(handler, "GET", "/api/ui/config", server.token, "127.0.0.1:40123", cookies, nil)
	if config.Code != http.StatusOK {
		t.Fatalf("owner config read: %d %s", config.Code, config.Body.String())
	}
	home := clientRequest(handler, "GET", "/api/desktop/home", server.token, "127.0.0.1:40123", cookies, nil)
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), `"featured"`) {
		t.Fatalf("home: %d %s", home.Code, home.Body.String())
	}
	episodes := clientRequest(handler, "GET", "/api/desktop/episodes?id="+historyFixtureDramaID, server.token, "127.0.0.1:40123", cookies, nil)
	if episodes.Code != http.StatusOK || !strings.Contains(episodes.Body.String(), `"total":3`) {
		t.Fatalf("episodes: %d %s", episodes.Code, episodes.Body.String())
	}
	// 写管理员接口仍需 X-Juku-Viewer（CSRF）
	if code := clientRequest(handler, "POST", "/api/ui/ffmpeg", server.token, "127.0.0.1:40123", cookies, nil).Code; code != http.StatusForbidden {
		t.Fatalf("admin write without viewer header: %d", code)
	}
}

func TestHTTPModeStillRequiresAdministrator(t *testing.T) {
	app := viewerTestApp(t)
	if err := app.prepareBrowserViewers(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "http://127.0.0.1:8999/api/ui/config", nil)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("server mode must keep admin requirement: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestClientLegacyDataDirectoryIsMigrated(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(base, clientLegacyAppName)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "marker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, clientAppName)
	moved, err := migrateLegacyClientDataDirectory(target)
	if err != nil || !moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(target, "marker")); err != nil {
		t.Fatal(err)
	}
	if directoryExists(legacy) {
		t.Fatal("legacy directory still exists")
	}
	// 新目录已存在时不再迁移。
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if moved, _ := migrateLegacyClientDataDirectory(target); moved {
		t.Fatal("should not migrate when target exists")
	}
}

func TestClientLegacyOutputDirectoryIsMigrated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	legacy := clientOutputDirectoryNamed(clientLegacyAppName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "ep1.mp4"), []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	if err := saveRuntimeSettings(data, runtimeSettings{Concurrency: 2, RequestConcurrency: 2, RequestIntervalMS: 500, OutputDir: &legacy}); err != nil {
		t.Fatal(err)
	}
	moved, err := migrateLegacyClientOutputDirectory(data)
	if err != nil || moved != clientDefaultOutputDirectory() {
		t.Fatalf("moved=%q err=%v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(moved, "ep1.mp4")); err != nil {
		t.Fatal(err)
	}
	document, err := readConfigDocument(runtimeSettingsPath(data))
	if err != nil || document.Download.Directory != moved {
		t.Fatalf("directory=%q err=%v", document.Download.Directory, err)
	}
	// 自定义目录不迁移。
	custom := filepath.Join(home, "elsewhere")
	if err := saveRuntimeSettings(data, runtimeSettings{Concurrency: 2, RequestConcurrency: 2, RequestIntervalMS: 500, OutputDir: &custom}); err != nil {
		t.Fatal(err)
	}
	if moved, _ := migrateLegacyClientOutputDirectory(data); moved != "" {
		t.Fatalf("custom directory migrated to %q", moved)
	}
}
