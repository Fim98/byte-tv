package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCoverLinksCarryDramaIdentity(t *testing.T) {
	address := "https://covers.example.org/old?signature=a%2Fb%2B%3D"
	drama := Drama{ID: "hongguo:7000000000000000001", Source: sourceHongguo, CoverURL: address}
	normalizeDramaCover(&drama)
	for _, local := range []string{drama.Cover.(string), currentCoverResult(drama).Cover} {
		parsed, err := url.Parse(local)
		if err != nil || parsed.Query().Get("dramaId") != drama.ID || parsed.Query().Get("url") != address {
			t.Fatal("cover link lost its drama identity or changed the signed address", local)
		}
	}
}

func TestCoverTargetFollowsMetadataWithoutFetchingStaleAddress(t *testing.T) {
	const oldAddress = "https://covers.example.org/old?signature=a%2Fb%2B%3D"
	const freshAddress = "https://new-covers.example.org/current?signature=c%2Fd%2B%3D"
	const id = "hongguo:7000000000000000001"
	var requested []string
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		requested = append(requested, request.URL.String())
		return rankingHTTPResponse(request, http.StatusServiceUnavailable, "text-only upstream fixture"), nil
	})
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{{ID: id, Source: sourceHongguo, CoverURL: freshAddress}}}
	for _, observed := range []string{oldAddress, "https://unregistered.example.org/unused", freshAddress} {
		local := "/api/ui/image?url=" + url.QueryEscape(observed) + "&dramaId=" + url.QueryEscape(id)
		request := httptest.NewRequest(http.MethodGet, local, nil)
		request = request.WithContext(withSourceScope(context.Background(), accountRecord{Sources: []string{sourceHongguo}}))
		writer := httptest.NewRecorder()
		app.handleImage(writer, request)
		if writer.Code != http.StatusBadGateway || !strings.Contains(writer.Body.String(), "上游 HTTP 503") {
			t.Fatal("known drama was rejected before the text fixture transport", writer.Code, writer.Body.String())
		}
	}
	if len(requested) != 3 {
		t.Fatal("unexpected cover request count", requested)
	}
	for _, address := range requested {
		if address != freshAddress {
			t.Fatal("fetched the stale or caller-supplied address", address)
		}
	}
}

func TestCoverTargetRetainsRegistrationAndPermissions(t *testing.T) {
	const id = "hongguo:7000000000000000001"
	const address = "https://covers.example.org/current"
	var calls int
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		return rankingHTTPResponse(request, http.StatusServiceUnavailable, "text-only upstream fixture"), nil
	})
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{{ID: id, Source: sourceHongguo, CoverURL: address}}}
	for _, test := range []struct {
		name, id, address string
		denied            bool
		status            int
	}{
		{"legacy-current", "", address, false, http.StatusBadGateway},
		{"legacy-stale", "", "https://covers.example.org/old", false, http.StatusForbidden},
		{"missing-drama", "hongguo:7000000000000000009", address, false, http.StatusForbidden},
		{"source-permission", id, address, true, http.StatusForbidden},
		{"private-parameter", id, "https://127.0.0.1/blocked", false, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			allowed := []string{sourceHongguo}
			if test.denied {
				allowed = []string{}
			}
			request := httptest.NewRequest(http.MethodGet, "/api/ui/image?url="+url.QueryEscape(test.address)+"&dramaId="+url.QueryEscape(test.id), nil)
			request = request.WithContext(withSourceScope(context.Background(), accountRecord{Sources: allowed}))
			writer := httptest.NewRecorder()
			before := calls
			app.handleImage(writer, request)
			if writer.Code != test.status || test.status != http.StatusBadGateway && calls != before {
				t.Fatal("cover registration or permissions were bypassed", writer.Code, calls-before)
			}
		})
	}
}
