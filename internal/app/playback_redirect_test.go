package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaybackAndDownloadHLSKeepRedirectedPlaylistBase(t *testing.T) {
	const origin = "https://origin.example.test"
	const cdn = "https://cdn.example.test"
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"main\",DEFAULT=YES,URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=720x1280,AUDIO=\"audio\"\nvariant.m3u8\n"
	variant := "#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"enc.key\"\n#EXTINF:3,\npart.m4s\n#EXT-X-ENDLIST\n"
	audio := "#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXTINF:3,\naudio.aac\n#EXT-X-ENDLIST\n"
	for _, resolver := range []string{"playback", "download"} {
		for _, kind := range []string{"master", "media"} {
			t.Run(resolver+"/"+kind, func(t *testing.T) {
				requested := map[string]int{}
				redirects := map[string]string{
					origin + "/master.m3u8":       cdn + "/catalog/master.m3u8",
					origin + "/media.m3u8":        cdn + "/rendition/index.m3u8",
					cdn + "/catalog/variant.m3u8": "/rendition/index.m3u8",
					cdn + "/catalog/audio.m3u8":   "/sound/index.m3u8",
				}
				bodies := map[string]string{
					cdn + "/catalog/master.m3u8":  master,
					cdn + "/rendition/index.m3u8": variant,
					cdn + "/sound/index.m3u8":     audio,
					cdn + "/rendition/init.mp4":   "synthetic-init",
					cdn + "/rendition/enc.key":    "0123456789abcdef",
					cdn + "/rendition/part.m4s":   "synthetic-video",
					cdn + "/sound/audio.aac":      "synthetic-audio",
				}
				downloader := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
					address := request.URL.String()
					requested[address]++
					if location := redirects[address]; location != "" {
						response := rankingHTTPResponse(request, http.StatusFound, "")
						response.Header.Set("Location", location)
						return response, nil
					}
					body, ok := bodies[address]
					if !ok {
						t.Errorf("relative HLS resource used the wrong base: %s", address)
						return rankingHTTPResponse(request, http.StatusNotFound, "wrong base"), nil
					}
					response := rankingHTTPResponse(request, http.StatusOK, body)
					if strings.HasPrefix(body, "#EXTM3U") {
						response.Header.Set("Content-Type", "application/vnd.apple.mpegurl")
					}
					return response, nil
				})
				ctx := context.Background()
				entry := origin + "/" + kind + ".m3u8"
				var media providerMedia
				var err error
				task := Task{DramaID: historyFixtureDramaID, Chapter: Chapter{Source: sourceHongguo, VideoURL: entry}}
				if resolver == "playback" {
					media, _, err = downloader.resolvePlaybackMedia(ctx, task)
				} else {
					media, err = downloader.resolveProviderMedia(ctx, task)
				}
				if err != nil || media.URL != redirects[entry] {
					t.Fatalf("preloaded playlist lost its final URL: %s %v", media.URL, err)
				}
				media, err = downloader.selectDownloadQuality(ctx, Task{}, media)
				if err != nil {
					t.Fatal(err)
				}
				proxy := &hlsProxy{client: downloader.client, base: "/media/", assets: map[string]hlsAsset{}, assetIDs: map[string]string{}}
				root := proxy.addAsset(playbackRootAsset(media))
				visited := map[string]bool{}
				var walk func(string)
				walk = func(address string) {
					t.Helper()
					if visited[address] {
						return
					}
					visited[address] = true
					if !strings.HasPrefix(address, "/media/") {
						t.Fatal("unproxied HLS reference", address)
					}
					response := httptest.NewRecorder()
					proxy.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://library.test"+address, nil))
					if response.Code != http.StatusOK {
						t.Fatalf("HLS resource failed: %s HTTP %d %s", address, response.Code, response.Body.String())
					}
					if !strings.HasPrefix(response.Body.String(), "#EXTM3U") {
						return
					}
					for _, line := range strings.Split(response.Body.String(), "\n") {
						if line != "" && !strings.HasPrefix(line, "#") {
							walk(line)
						}
						for _, attribute := range hlsURIAttribute.FindAllStringSubmatch(line, -1) {
							walk(attribute[1])
						}
					}
				}
				walk(root)
				for _, resource := range []string{"init.mp4", "enc.key", "part.m4s"} {
					if requested[cdn+"/rendition/"+resource] != 1 {
						t.Fatal("missing redirected media resource", resource, requested)
					}
				}
				if requested[entry] != 1 || proxy.Err() != nil || kind == "master" && requested[cdn+"/sound/audio.aac"] != 1 {
					t.Fatal("preloaded root or separate audio was mishandled", requested, proxy.Err())
				}
			})
		}
	}
}
