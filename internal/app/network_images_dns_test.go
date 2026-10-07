package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloaderCoverDNSFallbackUsesConfiguredProxy(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodConnect || request.Host != "dns.alidns.com:443" && request.Host != "dns.google:443" {
			t.Error("unexpected DNS proxy destination", request.Method, request.Host)
		}
		http.Error(writer, "local DNS failure fixture", http.StatusBadGateway)
	}))
	defer proxy.Close()
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.ProxyURL = t.TempDir(), t.TempDir(), proxy.URL
	downloader := NewDownloader(cfg)
	defer downloader.client.CloseIdleConnections()
	transport := downloader.client.Transport.(*imageTransport)
	transport.lookup = func(context.Context, string) ([]string, error) {
		return []string{"198.18.0.42"}, nil
	}
	app := &UIApp{downloader: downloader}
	ctx := context.WithValue(context.Background(), coverSourceKey{}, sourceHongguo)
	_, err := app.loadCoverImage(ctx, "https://covers.example.org/fixture", nil)
	if err == nil || !strings.Contains(err.Error(), "可信 DNS 重解析失败") || calls.Load() != 2 {
		t.Fatal("registered cover did not use the default DNS fallback through its configured proxy", calls.Load(), err)
	}
}

func TestDownloaderCoverDNSFallbackRecoversPublicOrigin(t *testing.T) {
	for _, test := range []struct {
		name      string
		addresses []string
		err       error
		dnsCalls  int32
	}{
		{name: "public-dns", addresses: []string{"93.184.216.34"}},
		{name: "mihomo-fake-ip", addresses: []string{"198.18.0.42"}, dnsCalls: 1},
		{name: "docker-private-ip", addresses: []string{"172.18.0.2"}, dnsCalls: 1},
		{name: "private-ipv6", addresses: []string{"fc00::1"}, dnsCalls: 1},
		{name: "mapped-private-ipv4", addresses: []string{"::ffff:192.168.1.1"}, dnsCalls: 1},
		{name: "mixed-dns", addresses: []string{"93.184.216.35", "198.18.0.42"}, dnsCalls: 1},
		{name: "empty-dns", dnsCalls: 1},
		{name: "dns-error", err: errors.New("local DNS unavailable"), dnsCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var dnsCalls, targetCalls, dials atomic.Int32
			target := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				targetCalls.Add(1)
				if request.Host != "example.com" || request.TLS.ServerName != "example.com" || request.URL.RawQuery != "signature=%2F%2b%3D" || request.Header.Get("Referer") != "https://source.example.org/" {
					t.Error("DNS recovery changed the original Host, SNI, signature or source header")
				}
				io.WriteString(writer, "local text transport fixture")
			}))
			defer target.Close()
			dns := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				dnsCalls.Add(1)
				if request.URL.Query().Get("name") != "example.com" {
					t.Error("unexpected DNS hostname", request.URL)
				}
				writeDNSFixture(writer, "93.184.216.34")
			}))
			defer dns.Close()
			cfg := defaultConfig()
			cfg.dataDir, cfg.OutputDir, cfg.ProxyURL = t.TempDir(), t.TempDir(), "direct"
			downloader := NewDownloader(cfg)
			defer downloader.client.CloseIdleConnections()
			transport := downloader.client.Transport.(*imageTransport)
			transport.lookup = func(context.Context, string) ([]string, error) { return test.addresses, test.err }
			transport.resolver.endpoints = []string{dns.URL}
			transport.standard.TLSClientConfig = target.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			transport.standard.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				if address != "93.184.216.34:443" {
					t.Error("attempted a connection outside the verified public destination", address)
					return nil, errors.New("unexpected connection")
				}
				return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx = context.WithValue(context.WithValue(ctx, coverRequestKey{}, true), coverSourceKey{}, sourceHongguo)
			for range 2 {
				request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/fixture?signature=%2F%2b%3D", nil)
				request.Header.Set("Referer", "https://source.example.org/")
				response, err := downloader.client.Do(request)
				if err != nil {
					t.Fatal("default cover DNS recovery failed", err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || string(body) != "local text transport fixture" || response.Request.URL.String() != request.URL.String() {
					t.Fatal("DNS recovery changed the response or original URL", err)
				}
			}
			if dnsCalls.Load() != test.dnsCalls || targetCalls.Load() != 2 || dials.Load() != 1 {
				t.Fatal("unexpected DNS or connection reuse", dnsCalls.Load(), targetCalls.Load(), dials.Load())
			}
		})
	}
}

func TestDownloaderCoverDNSFallbackRejectsUnsafeDestinations(t *testing.T) {
	for _, test := range []struct {
		name      string
		url       string
		source    string
		addresses []string
		redirect  bool
		dnsCalls  int32
	}{
		{name: "literal-private", url: "https://10.0.0.1/fixture", source: sourceHongguo},
		{name: "literal-fake-ip", url: "https://198.18.0.42/fixture", source: sourceHongguo},
		{name: "unregistered-source", url: "https://example.com/fixture"},
		{name: "private-answer", url: "https://example.com/fixture", source: sourceHongguo, addresses: []string{"127.0.0.1"}, dnsCalls: 2},
		{name: "mixed-answer", url: "https://example.com/fixture", source: sourceHongguo, addresses: []string{"93.184.216.34", "10.0.0.1"}, dnsCalls: 2},
		{name: "private-redirect", url: "https://example.com/fixture", source: sourceHongguo, addresses: []string{"169.254.169.254"}, redirect: true, dnsCalls: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var dnsCalls, targetCalls atomic.Int32
			target := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				targetCalls.Add(1)
				http.Redirect(writer, request, "https://redirect.example.com/fixture", http.StatusFound)
			}))
			defer target.Close()
			dns := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				dnsCalls.Add(1)
				if test.redirect && request.URL.Query().Get("name") == "example.com" {
					writeDNSFixture(writer, "93.184.216.34")
					return
				}
				writeDNSFixture(writer, test.addresses...)
			}))
			defer dns.Close()
			cfg := defaultConfig()
			cfg.dataDir, cfg.OutputDir, cfg.ProxyURL = t.TempDir(), t.TempDir(), "direct"
			downloader := NewDownloader(cfg)
			defer downloader.client.CloseIdleConnections()
			transport := downloader.client.Transport.(*imageTransport)
			transport.lookup = func(context.Context, string) ([]string, error) { return []string{"198.18.0.42"}, nil }
			transport.resolver.endpoints = []string{dns.URL + "/first", dns.URL + "/second"}
			transport.standard.TLSClientConfig = target.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			transport.standard.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if !test.redirect || address != "93.184.216.34:443" {
					t.Error("unsafe destination reached the dialer", address)
					return nil, errors.New("unexpected connection")
				}
				return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx = context.WithValue(context.WithValue(ctx, coverRequestKey{}, true), coverSourceKey{}, test.source)
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, test.url, nil)
			response, err := downloader.client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			wantTargets := int32(0)
			if test.redirect {
				wantTargets = 1
			}
			if err == nil || dnsCalls.Load() != test.dnsCalls || targetCalls.Load() != wantTargets {
				t.Fatal("unsafe cover resolution or redirect was accepted", dnsCalls.Load(), targetCalls.Load(), err)
			}
		})
	}
}
