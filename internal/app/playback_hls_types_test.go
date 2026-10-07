package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHLSGatewayPreservesMediaTypes(t *testing.T) {
	proxy := &hlsProxy{base: "/media/", assets: map[string]hlsAsset{}, assetIDs: map[string]string{}, client: &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader("synthetic bytes")), Request: request}, nil
	})}}
	playlist, err := proxy.rewritePlaylist("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"secret\"\n#EXTINF:3,\npart.m4s\n#EXTINF:3,\npart.ts\n#EXT-X-ENDLIST\n", "https://media.example.org/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	var addresses []string
	for _, match := range hlsURIAttribute.FindAllStringSubmatch(playlist, -1) {
		addresses = append(addresses, match[1])
	}
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "/media/") {
			addresses = append(addresses, line)
		}
	}
	expected := []struct{ extension, contentType string }{{".mp4", "video/mp4"}, {".key", "application/octet-stream"}, {".m4s", "video/mp4"}, {".ts", "video/mp2t"}}
	if len(addresses) != len(expected) {
		t.Fatal(playlist)
	}
	for index, address := range addresses {
		writer := httptest.NewRecorder()
		proxy.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "http://localhost"+address, nil))
		if !strings.HasSuffix(address, expected[index].extension) || writer.Header().Get("Content-Type") != expected[index].contentType || writer.Code != 200 || writer.Body.String() != "synthetic bytes" {
			t.Fatal("HLS asset was mislabeled or changed", address, writer.Code, writer.Header())
		}
	}
}
