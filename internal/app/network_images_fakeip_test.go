package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegisteredCoverRecoversFromFakeIPWithoutPrivateConnections(t *testing.T) {
	for _, mode := range []string{"public-fallback", "private-fallback", "mixed-fallback", "private-redirect"} {
		t.Run(mode, func(t *testing.T) {
			data := embySyntheticPoster(t)
			var calls, fallbacks atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Host != "p3-novel.byteimg.com" || request.TLS.ServerName != "p3-novel.byteimg.com" || request.Header.Get("Referer") == "" {
					t.Error("cover lost its original Host, SNI or source headers")
				}
				if mode == "private-redirect" {
					http.Redirect(writer, request, "https://private.edge.example.org/cover", http.StatusFound)
					return
				}
				writer.Write(data)
			}))
			defer server.Close()
			standard := http.DefaultTransport.(*http.Transport).Clone()
			standard.Proxy = nil
			standard.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			standard.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				calls.Add(1)
				if address != "93.184.216.34:443" {
					t.Error("attempted a connection to unverified DNS", address)
					return nil, errors.New("unverified connection")
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			transport := newImageTransport(standard, standard)
			defer transport.CloseIdleConnections()
			transport.lookup = func(context.Context, string) ([]string, error) {
				return []string{"198.18.0.42"}, nil
			}
			transport.fallback = func(ctx context.Context, host string) ([]string, error) {
				fallbacks.Add(1)
				if ctx.Value(coverSourceKey{}) != sourceHongguo {
					t.Error("shared image cache dropped the registered source context")
				}
				if mode == "private-fallback" || host == "private.edge.example.org" {
					return []string{"127.0.0.1"}, nil
				}
				if mode == "mixed-fallback" {
					return []string{"93.184.216.34", "10.0.0.1"}, nil
				}
				return []string{"93.184.216.34"}, nil
			}
			app := &UIApp{downloader: &Downloader{client: &http.Client{Transport: transport, Timeout: 3 * time.Second}}}
			ctx := context.WithValue(context.Background(), coverSourceKey{}, sourceHongguo)
			body, err := app.loadCoverImage(ctx, "https://p3-novel.byteimg.com/novel-pic/fixture.image", nil)
			if mode == "public-fallback" {
				if err != nil || !bytes.Equal(body, data) || calls.Load() != 1 || fallbacks.Load() != 1 {
					t.Fatal("registered cover did not recover safely from Fake-IP", calls.Load(), fallbacks.Load(), err)
				}
				if _, err := app.loadCoverImage(ctx, "https://p3-novel.byteimg.com/novel-pic/fixture.image", nil); err != nil || calls.Load() != 1 {
					t.Fatal("successful cover was not cached", err)
				}
			} else if err == nil || mode != "private-redirect" && calls.Load() != 0 || mode == "private-redirect" && calls.Load() != 1 {
				t.Fatal("unsafe fallback or redirect reached the network", calls.Load(), err)
			}
		})
	}
}
