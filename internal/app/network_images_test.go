package app

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestImageURLAcceptsPublicCDNsWithoutDomainList(t *testing.T) {
	for _, address := range []string{"https://new-covers.example.org/a?token=%2F%2b%3D", "https://another.example.com/cover", "https://93.184.216.34/image", "https://[2606:4700:4700::1111]/image"} {
		parsed, err := url.Parse(address)
		if err != nil || !validImageURL(parsed) {
			t.Fatal("public cover URL required a hard-coded domain", address, err)
		}
	}
	for _, address := range []string{
		"http://example.org/a", "file:///etc/passwd", "https://user@example.org/a", "https://example.org:8443/a",
		"https://localhost/a", "https://app.localhost/a", "https://router.local/a", "https://private.lan/a",
		"https://127.0.0.1/a", "https://10.1.2.3/a", "https://172.16.0.1/a", "https://192.168.1.1/a",
		"https://169.254.169.254/a", "https://100.100.100.200/a", "https://0.0.0.0/a", "https://255.255.255.255/a",
		"https://[::1]/a", "https://[::ffff:127.0.0.1]/a", "https://[fc00::1]/a", "https://[fe80::1%25en0]/a",
		"https://[64:ff9b::a00:1]/a", "https://example.invalid/a", "https://empty../a",
	} {
		parsed, err := url.Parse(address)
		if err == nil && validImageURL(parsed) {
			t.Fatal("unsafe cover URL was accepted", address)
		}
	}
}

func TestImageTransportRejectsPrivateDNSBeforeAnyConnection(t *testing.T) {
	for _, addresses := range [][]string{{"127.0.0.1"}, {"10.0.0.1"}, {"100.100.100.200"}, {"::ffff:192.168.1.1"}, {"93.184.216.34", "169.254.169.254"}} {
		t.Run(strings.Join(addresses, "/"), func(t *testing.T) {
			var calls atomic.Int32
			standard := http.DefaultTransport.(*http.Transport).Clone()
			standard.DialContext = func(context.Context, string, string) (net.Conn, error) {
				calls.Add(1)
				return nil, errors.New("must not connect")
			}
			transport := newImageTransport(standard, standard)
			transport.lookup = func(context.Context, string) ([]string, error) { return addresses, nil }
			transport.fallback = func(context.Context, string) ([]string, error) {
				calls.Add(1)
				return []string{"93.184.216.34"}, nil
			}
			request := httptest.NewRequest(http.MethodGet, "https://rebound.example.org/cover", nil)
			request = request.WithContext(context.WithValue(request.Context(), coverRequestKey{}, true))
			if _, err := transport.RoundTrip(request); err == nil || calls.Load() != 0 {
				t.Fatal("private DNS was fetched or retried as a public destination", calls.Load(), err)
			}
		})
	}
}

func TestImageTransportPinsDNSPreservesSNIAndProxy(t *testing.T) {
	for _, mode := range []string{"direct", "http-proxy", "https-proxy"} {
		t.Run(mode, func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				if r.Host != "new-covers.example.org" || r.TLS.ServerName != "new-covers.example.org" || r.URL.RawQuery != "signature=%2F%2b%3D" || r.Header.Get("Referer") != "https://source.example.org/" {
					t.Error("pinned image request changed the hostname, TLS name, query or source headers")
				}
				io.WriteString(w, "synthetic transport fixture")
			}))
			defer target.Close()
			var mu sync.Mutex
			var dials, connects []string
			proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				connects = append(connects, r.Host)
				mu.Unlock()
				if r.Method != http.MethodConnect || r.Host != "93.184.216.34:443" {
					http.Error(w, "unexpected CONNECT destination", http.StatusBadRequest)
					return
				}
				upstream, err := net.Dial("tcp", target.Listener.Addr().String())
				if err != nil {
					t.Error(err)
					return
				}
				connection, buffer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					upstream.Close()
					t.Error(err)
					return
				}
				buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				buffer.Flush()
				go func() { io.Copy(upstream, buffer); upstream.Close() }()
				io.Copy(connection, upstream)
				connection.Close()
			})
			standard := http.DefaultTransport.(*http.Transport).Clone()
			standard.Proxy = nil
			standard.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			var proxyAddress string
			if mode != "direct" {
				var proxy *httptest.Server
				if mode == "https-proxy" {
					proxy = httptest.NewTLSServer(proxyHandler)
				} else {
					proxy = httptest.NewServer(proxyHandler)
				}
				defer proxy.Close()
				proxyURL, _ := url.Parse(proxy.URL)
				proxyAddress = proxyURL.Host
				standard.Proxy = http.ProxyURL(proxyURL)
			}
			standard.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				mu.Lock()
				dials = append(dials, address)
				mu.Unlock()
				if mode == "direct" && address == "93.184.216.34:443" {
					address = target.Listener.Addr().String()
				} else if mode == "direct" || address != proxyAddress {
					return nil, errors.New("request bypassed its pinned address or configured proxy")
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			transport := newImageTransport(standard, standard)
			defer transport.CloseIdleConnections()
			transport.lookup = func(ctx context.Context, host string) ([]string, error) {
				if host != "new-covers.example.org" {
					t.Error("unexpected hostname", host)
				}
				return []string{"93.184.216.34"}, nil
			}
			request, _ := http.NewRequestWithContext(context.WithValue(context.Background(), coverRequestKey{}, true), http.MethodGet, "https://new-covers.example.org/cover?signature=%2F%2b%3D", nil)
			request.Header.Set("Referer", "https://source.example.org/")
			response, err := (&http.Client{Transport: transport}).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || string(body) != "synthetic transport fixture" || response.Request.URL.String() != request.URL.String() {
				t.Fatal("pinned response lost the original address", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if targetCalls.Load() != 1 || len(dials) != 1 || mode == "direct" && len(connects) != 0 || mode != "direct" && !reflect.DeepEqual(connects, []string{"93.184.216.34:443"}) {
				t.Fatal("image request did not follow the expected pinned route", dials, connects, targetCalls.Load())
			}
		})
	}
}

func TestImageSourceRequiresRegisteredCoverForEveryViewer(t *testing.T) {
	address := "https://new-covers.example.org/cover?token=fixture"
	app := &UIApp{dramas: []Drama{{ID: "hongguo:7000000000000000001", Source: sourceHongguo, CoverURL: address}}}
	for _, ctx := range []context.Context{context.Background(), withSourceScope(context.Background(), accountRecord{Admin: true}), withSourceScope(context.Background(), accountRecord{Sources: []string{sourceHongguo}})} {
		if source, allowed := app.imageSource(ctx, address); !allowed || source != sourceHongguo {
			t.Fatal("registered cover was rejected")
		}
		if _, allowed := app.imageSource(ctx, address+"-forged"); allowed {
			t.Fatal("unregistered cover was accepted")
		}
	}
}
