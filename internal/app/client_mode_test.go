package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
