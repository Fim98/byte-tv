package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func writeDNSFixture(writer http.ResponseWriter, addresses ...string) {
	answers := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		answers = append(answers, map[string]any{"type": 1, "TTL": 37, "data": address})
	}
	writer.Header().Set("Content-Type", "application/dns-json")
	json.NewEncoder(writer).Encode(map[string]any{"Status": 0, "Answer": answers})
}

func TestDNSResolverRetriesUnsafeOrInvalidAnswers(t *testing.T) {
	for _, test := range []struct {
		name      string
		addresses []string
		body      string
		status    int
	}{
		{name: "fake-ip", addresses: []string{"198.18.0.42"}},
		{name: "private", addresses: []string{"172.18.0.2"}},
		{name: "loopback", addresses: []string{"127.0.0.1"}},
		{name: "metadata", addresses: []string{"169.254.169.254"}},
		{name: "carrier-network", addresses: []string{"100.64.0.1"}},
		{name: "reserved", addresses: []string{"203.0.113.1"}},
		{name: "mixed-private", addresses: []string{"93.184.216.35", "10.0.0.1"}},
		{name: "mixed-fake-ip", addresses: []string{"93.184.216.35", "198.19.0.1"}},
		{name: "invalid-address", addresses: []string{"93.184.216.35", "invalid address"}},
		{name: "ipv6-in-a-record", addresses: []string{"2606:4700:4700::1111"}},
		{name: "empty-answer"},
		{name: "nxdomain", body: `{"Status":3}`},
		{name: "malformed-json", body: `{"Status":`},
		{name: "http-error", addresses: []string{"93.184.216.35"}, status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			var firstCalls, secondCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				query := request.URL.Query()
				if query.Get("name") != "covers.example.org" || query.Get("type") != "A" || query.Get("edns_client_subnet") != dnsAlternateSubnet || request.Header.Get("Accept") != "application/dns-json" {
					t.Error("unexpected DNS request", request.URL)
				}
				if request.URL.Path == "/first" {
					firstCalls.Add(1)
					if test.status != 0 {
						writer.WriteHeader(test.status)
					}
					if test.body != "" {
						io.WriteString(writer, test.body)
					} else {
						writeDNSFixture(writer, test.addresses...)
					}
					return
				}
				secondCalls.Add(1)
				writeDNSFixture(writer, "93.184.216.34")
			}))
			defer server.Close()
			standard := http.DefaultTransport.(*http.Transport).Clone()
			standard.Proxy = nil
			resolver := newDNSResolver(standard)
			defer resolver.client.CloseIdleConnections()
			resolver.endpoints = []string{server.URL + "/first", server.URL + "/second"}
			for range 2 {
				entry, err := resolver.lookupEntry(context.Background(), "covers.example.org", dnsAlternateSubnet)
				if err != nil || !reflect.DeepEqual(entry.addresses, []string{"93.184.216.34"}) {
					t.Fatal("DNS failed to continue to a valid public answer", entry.addresses, err)
				}
				if remaining := time.Until(entry.expires); remaining <= 0 || remaining > 37*time.Second {
					t.Fatal("DNS cache ignored the answer TTL", remaining)
				}
			}
			if firstCalls.Load() != 1 || secondCalls.Load() != 1 {
				t.Fatal("DNS fallback did not reuse its successful cache", firstCalls.Load(), secondCalls.Load())
			}
		})
	}
}

func TestDNSResolverDoesNotCacheUnsafeAnswers(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writeDNSFixture(writer, "93.184.216.34", "198.18.0.42")
	}))
	defer server.Close()
	standard := http.DefaultTransport.(*http.Transport).Clone()
	standard.Proxy = nil
	resolver := newDNSResolver(standard)
	defer resolver.client.CloseIdleConnections()
	resolver.endpoints = []string{server.URL + "/first", server.URL + "/second"}
	for range 2 {
		addresses, err := resolver.lookup(context.Background(), "covers.example.org")
		if err == nil || len(addresses) != 0 {
			t.Fatal("unsafe DNS answer was returned", addresses, err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal("unsafe DNS answer was cached or prevented trying another service", calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.lookup(ctx, "cancelled.example.org"); !errors.Is(err, context.Canceled) || calls.Load() != 4 {
		t.Fatal("cancelled DNS lookup did not stop", calls.Load(), err)
	}
}
