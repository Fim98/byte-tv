package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParityMediaCredentialsStayOnExactOrigin(t *testing.T) {
	var requests int
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Header.Get("Sec-Fetch-Mode") != "cors" || request.Header.Get("Sec-Fetch-Dest") != "empty" || request.Header.Get("Referer") != "https://site.example.test/episode/1" {
			t.Error("media request lost required headers")
		}
		response := rankingHTTPResponse(request, 200, "fixture")
		if request.URL.Host == "media.example.test" {
			if request.Header.Get("Cookie") != "Signed=fixture" {
				t.Error("credential missing on its origin")
			}
			response.StatusCode = 302
			response.Header.Set("Location", "https://other.example.test/segment")
		} else if request.Header.Get("Cookie") != "" {
			t.Error("signed credential leaked on redirect")
		}
		return response, nil
	})
	credentials := &providerMediaCredentials{origin: "https://media.example.test", cookie: "Signed=fixture", referer: "https://site.example.test/episode/1", expires: time.Now().Add(time.Minute)}
	request, _ := http.NewRequestWithContext(providerMediaContext(context.Background(), credentials), http.MethodGet, "https://media.example.test/segment", nil)
	response, err := d.doMediaRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if requests != 2 || request.Header.Get("Cookie") != "" {
		t.Fatal("redirect not followed or caller request mutated")
	}
	credentials.expires = time.Now().Add(-time.Second)
	if _, err := d.doMediaRequest(request); err == nil || requests != 2 {
		t.Fatal("expired credentials reached the network")
	}
	for _, address := range []string{"http://media.example.test/segment", "https://media.example.test:444/segment", "https://child.media.example.test/segment"} {
		other, _ := http.NewRequest(http.MethodGet, address, nil)
		other.Header.Set("Cookie", "Signed=fixture")
		if err := credentials.apply(other); err != nil || other.Header.Get("Cookie") != "" {
			t.Fatal("origin scope includes another scheme, port or subdomain")
		}
	}
}

func TestParityMediaProxiesKeepCredentialsForListsKeysAndSegments(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		t.Run(fmt.Sprint(gateway), func(t *testing.T) {
			seen := map[string]int{}
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				seen[request.URL.Path]++
				if request.Header.Get("Cookie") != "Signed=fixture" || request.Header.Get("Sec-Fetch-Mode") != "cors" {
					t.Error("proxy dropped credentials or media headers")
				}
				switch request.URL.Path {
				case "/start.m3u8":
					response := rankingHTTPResponse(request, 302, "")
					response.Header.Set("Location", "/final/index.m3u8?sig=a%2Fb")
					return response, nil
				case "/final/index.m3u8":
					return rankingHTTPResponse(request, 200, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n"), nil
				case "/final/key":
					return rankingHTTPResponse(request, 200, "0123456789abcdef"), nil
				case "/final/segment.ts":
					return rankingHTTPResponse(request, 200, "synthetic-segment"), nil
				}
				return nil, fmt.Errorf("unexpected request: %s", request.URL.Path)
			})
			credentials := &providerMediaCredentials{origin: "https://media.example.test", cookie: "Signed=fixture", referer: "https://site.example.test/episode/1"}
			ctx := providerMediaContext(context.Background(), credentials)
			playlist, final, err := d.fetchMediaPlaylist(ctx, "https://media.example.test/start.m3u8", credentials.referer)
			if err != nil || final != "https://media.example.test/final/index.m3u8?sig=a%2Fb" {
				t.Fatal("redirect lost its final playlist base", final, err)
			}
			media := providerMedia{URL: final, Playlist: playlist, Referer: credentials.referer, credentials: credentials}
			proxy, err := d.newHLSProxy(context.Background(), media, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer proxy.Close()
			root := proxy.root
			if gateway {
				proxy.Close()
				session := &playbackMediaSession{media: media, plan: playbackMediaPlan{Player: "hls"}}
				app := &UIApp{downloader: d}
				app.attachMediaGateway(session, "/media/", "")
				proxy = session.proxy
				root = session.plan.URL
			}
			read := func(path string) string {
				t.Helper()
				writer := httptest.NewRecorder()
				proxy.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path, nil))
				if writer.Code != 200 {
					t.Fatalf("proxy response %d: %s", writer.Code, writer.Body.String())
				}
				return writer.Body.String()
			}
			body := read(root)
			key := hlsURIAttribute.FindStringSubmatch(body)
			if len(key) != 2 || read(key[1]) != "0123456789abcdef" {
				t.Fatal("rewritten key not readable")
			}
			segment := ""
			for _, line := range strings.Split(body, "\n") {
				if line != "" && !strings.HasPrefix(line, "#") {
					segment = line
					break
				}
			}
			if segment == "" || read(segment) != "synthetic-segment" || seen["/final/key"] != 1 || seen["/final/segment.ts"] != 1 {
				t.Fatal("rewritten segment not readable")
			}
		})
	}
}

func TestParityPlaybackQualityInheritsMediaCredentials(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Cookie") != "Signed=fixture" {
			t.Error("variant lost credentials")
		}
		return rankingHTTPResponse(request, 200, "#EXTM3U\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n"), nil
	})
	credentials := &providerMediaCredentials{origin: "https://media.example.test", cookie: "Signed=fixture"}
	media := providerMedia{URL: "https://media.example.test/high.mp4", Referer: "https://site.example.test/", Quality: 1080, credentials: credentials,
		Variants: []providerMedia{{URL: "https://media.example.test/low.m3u8", Quality: 720}}}
	selected, err := d.selectPlaybackQuality(context.WithValue(context.Background(), playbackQualityKey{}, 720), media)
	if err != nil || selected.credentials != credentials || selected.Referer != media.Referer || selected.Playlist == "" {
		t.Fatal("selected variant lost session or playlist", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://media.example.test:443/part", nil)
	if providerMediaOrigin(request.URL) != credentials.origin {
		t.Fatal("default port changed origin")
	}
	_, _, err = d.fetchMediaPlaylist(context.Background(), "file:///tmp/fixture", "")
	if err == nil {
		t.Fatal("non-HTTP playlist accepted")
	}
	_, _ = io.Discard.Write(nil)
	_, _ = url.Parse("https://example.test")
}
