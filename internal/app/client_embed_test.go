package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

type fakeHost struct{ actions []string }

func (h *fakeHost) ClientReady(int, string) {}
func (h *fakeHost) Native(action string, body json.RawMessage) (any, error) {
	h.actions = append(h.actions, action)
	if action == "boom" {
		panic("x")
	}
	return map[string]string{"echo": string(body)}, nil
}

func TestClientMediaTokenOnlyForPlaybackReads(t *testing.T) {
	server, _ := clientTestServer(t)
	server.mediaToken = mediaToken(server.token)
	handler := server.handler()
	bt := "?bt=" + server.mediaToken
	if code := clientRequest(handler, "GET", "/api/ui/playback/hls/segment.ts"+bt, "", "127.0.0.1:40123", nil, nil).Code; code == http.StatusUnauthorized {
		t.Fatalf("media token rejected on playback path")
	}
	if code := clientRequest(handler, "GET", "/api/desktop/status"+bt, "", "127.0.0.1:40123", nil, nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("media token must not unlock other APIs: %d", code)
	}
	if code := clientRequest(handler, "POST", "/api/ui/playback/plan"+bt, "", "127.0.0.1:40123", nil, nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("media token must not unlock writes: %d", code)
	}
}

func TestClientPrefsAndNative(t *testing.T) {
	server, handler := clientTestServer(t)
	request := httptest.NewRequest("POST", "http://127.0.0.1:40123/api/desktop/prefs", strings.NewReader(`{"juku.theme":"dark"}`))
	request.Host = "127.0.0.1:40123"
	request.Header.Set("X-Juku-Token", server.token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "dark") {
		t.Fatalf("prefs: %d %s", recorder.Code, recorder.Body.String())
	}
	got := clientRequest(handler, "GET", "/api/desktop/prefs", server.token, "127.0.0.1:40123", nil, nil)
	if !strings.Contains(got.Body.String(), `"juku.theme": "dark"`) && !strings.Contains(got.Body.String(), `"juku.theme":"dark"`) {
		t.Fatalf("prefs not persisted: %s", got.Body.String())
	}
	if code := clientRequest(handler, "POST", "/api/desktop/native/notify", server.token, "127.0.0.1:40123", nil, nil).Code; code != http.StatusNotImplemented {
		t.Fatalf("no host: %d", code)
	}
	host := &fakeHost{}
	SetClientHost(host)
	defer SetClientHost(nil)
	if code := clientRequest(handler, "POST", "/api/desktop/native/notify", server.token, "127.0.0.1:40123", nil, nil).Code; code != 200 || len(host.actions) != 1 {
		t.Fatalf("native: %d %v", code, host.actions)
	}
	if code := clientRequest(handler, "POST", "/api/desktop/native/boom", server.token, "127.0.0.1:40123", nil, nil).Code; code != http.StatusBadRequest {
		t.Fatalf("panic not recovered: %d", code)
	}
}

func TestSpaFSServerFallsBack(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte("<html>bt</html>")}, "assets/a.js": {Data: []byte("js")}}
	h := spaFSServer(files)
	for path, want := range map[string]string{"/": "bt", "/browse": "bt", "/assets/a.js": "js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s => %s", path, rec.Body.String())
		}
	}
}
