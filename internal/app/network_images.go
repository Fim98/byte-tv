package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

type coverRequestKey struct{}
type coverSourceKey struct{}

var nonPublicImageNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func publicImageAddress(value string) bool {
	address, err := netip.ParseAddr(value)
	if err != nil || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	for _, network := range nonPublicImageNetworks {
		if network.Contains(address) {
			return false
		}
	}
	return true
}

func validImageURL(remote *url.URL) bool {
	if remote == nil || remote.Scheme != "https" || remote.Opaque != "" || remote.User != nil ||
		(remote.Port() != "" && remote.Port() != "443") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(remote.Hostname(), "."))
	if _, err := netip.ParseAddr(host); err == nil {
		return publicImageAddress(host)
	}
	if len(host) > 253 || !strings.Contains(host, ".") || strings.ContainsAny(host, ":%\\") {
		return false
	}
	for _, suffix := range []string{"localhost", "local", "internal", "lan", "home", "invalid", "test", "example"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return false
		}
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
				return false
			}
		}
	}
	return true
}

type imageTransport struct {
	base       http.RoundTripper
	standard   *http.Transport
	resolver   *dnsResolver
	lookup     func(context.Context, string) ([]string, error)
	fallback   func(context.Context, string) ([]string, error)
	mu         sync.Mutex
	transports map[string]*http.Transport
}

func newImageTransport(base http.RoundTripper, standard *http.Transport) *imageTransport {
	resolver := newDNSResolver(standard)
	return &imageTransport{
		base: base, standard: standard, resolver: resolver,
		lookup: net.DefaultResolver.LookupHost, transports: map[string]*http.Transport{},
		fallback: func(ctx context.Context, host string) ([]string, error) {
			entry, err := resolver.lookupEntry(ctx, host, dnsAlternateSubnet)
			return entry.addresses, err
		},
	}
}

func (transport *imageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Context().Value(coverRequestKey{}) != true {
		return transport.base.RoundTrip(request)
	}
	if !validImageURL(request.URL) || request.Method != http.MethodGet {
		return nil, errors.New("封面地址不受支持")
	}
	host := strings.TrimSuffix(request.URL.Hostname(), ".")
	fallbackUsed := false
	fallback := func() ([]string, error) {
		fallbackUsed = true
		return transport.fallback(request.Context(), host)
	}
	addresses, err := transport.lookup(request.Context(), host)
	if (err != nil || len(addresses) == 0) && transport.fallback != nil && request.Context().Err() == nil {
		addresses, err = fallback()
	}
	if err != nil {
		return nil, fmt.Errorf("封面域名解析失败: %w", err)
	}
	var proxyURL *url.URL
	if transport.standard.Proxy != nil {
		proxyURL, err = transport.standard.Proxy(request)
		if err != nil {
			return nil, err
		}
	}
	tried := map[string]bool{}
	lastErr := errors.New("封面没有可用的公网地址")
	for attempt := 0; attempt < 2; attempt++ {
		if addressErr := validateImageAddresses(addresses); addressErr != nil {
			source, _ := request.Context().Value(coverSourceKey{}).(string)
			if fallbackUsed || transport.fallback == nil || accountSourceGroup(source) == "" || net.ParseIP(host) != nil || request.Context().Err() != nil {
				return nil, addressErr
			}

			addresses, err = fallback()
			if err != nil {
				return nil, fmt.Errorf("%v；可信 DNS 重解析失败：%w", addressErr, err)
			}
			if err = validateImageAddresses(addresses); err != nil {
				return nil, fmt.Errorf("可信 DNS 未提供可用封面地址：%w", err)
			}
		}
		for _, address := range addresses {
			if tried[address] || len(tried) >= 4 {
				continue
			}
			tried[address] = true
			pinned := transport.pinned(host, address, proxyURL)
			upstream := request.Clone(request.Context())
			upstream.Host = request.URL.Host
			upstream.URL.Host = net.JoinHostPort(address, "443")
			response, roundTripErr := pinned.RoundTrip(upstream)
			if response != nil {
				response.Request = request
			}
			if roundTripErr == nil {
				return response, nil
			}
			lastErr = roundTripErr
			if request.Context().Err() != nil {
				return nil, request.Context().Err()
			}
		}
		if fallbackUsed || transport.fallback == nil || len(tried) >= 4 {
			break
		}
		addresses, err = fallback()
		if err != nil {
			break
		}
	}
	return nil, fmt.Errorf("封面 CDN 连接失败: %w", lastErr)
}

func validateImageAddresses(addresses []string) error {
	if len(addresses) == 0 {
		return errors.New("封面没有可用的公网地址")
	}
	for _, address := range addresses {
		if !publicImageAddress(address) {
			return fmt.Errorf("封面地址指向本机、内网或保留网络（%s）", address)
		}
	}
	return nil
}

func (transport *imageTransport) pinned(host, address string, proxyURL *url.URL) *http.Transport {
	key := host + "|" + address
	if proxyURL != nil {
		key += "|" + proxyURL.String()
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if existing := transport.transports[key]; existing != nil {
		return existing
	}
	pinned := transport.standard.Clone()
	if pinned.TLSClientConfig == nil {
		pinned.TLSClientConfig = &tls.Config{}
	}
	pinned.TLSClientConfig.ServerName = host
	pinned.Proxy = http.ProxyURL(proxyURL)
	if proxyURL != nil && proxyURL.Scheme == "https" {
		plain := *proxyURL
		plain.Scheme = "http"
		if plain.Port() == "" {
			plain.Host = net.JoinHostPort(plain.Hostname(), "443")
		}
		pinned.Proxy = http.ProxyURL(&plain)
		pinned.DialContext = transport.tlsProxyDialer(proxyURL.Hostname())
	}
	if len(transport.transports) >= 32 {
		for key, stale := range transport.transports {
			stale.CloseIdleConnections()
			delete(transport.transports, key)
			break
		}
	}
	transport.transports[key] = pinned
	return pinned
}

func (transport *imageTransport) tlsProxyDialer(host string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		dial := transport.standard.DialContext
		if dial == nil {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			dial = dialer.DialContext
		}
		connection, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{}
		if transport.standard.TLSClientConfig != nil {
			config = transport.standard.TLSClientConfig.Clone()
		}
		config.ServerName, config.NextProtos = host, []string{"http/1.1"}
		secured := tls.Client(connection, config)
		if transport.standard.TLSHandshakeTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, transport.standard.TLSHandshakeTimeout)
			defer cancel()
		}
		if err := secured.HandshakeContext(ctx); err != nil {
			connection.Close()
			return nil, err
		}
		return secured, nil
	}
}

func (transport *imageTransport) CloseIdleConnections() {
	transport.resolver.client.CloseIdleConnections()
	if closer, ok := transport.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	for key, pinned := range transport.transports {
		pinned.CloseIdleConnections()
		delete(transport.transports, key)
	}
}
