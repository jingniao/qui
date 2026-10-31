// Package importer fetches and parses untrusted client subscriptions. Fetching
// belongs to the panel scheduler, never to the subscription request path.
package importer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

const MaxBody = 2 << 20

var ErrFetch = errors.New("subscription fetch refused or failed")

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type Fetcher struct {
	roots    *x509.CertPool
	Resolver Resolver
	// DialContext is injectable for tests; production always dials a validated IP.
	DialContext func(context.Context, string, string) (net.Conn, error)
	Timeout     time.Duration
}

// special holds non-public ranges that IsGlobalUnicast still accepts. The
// translation prefixes (NAT64, 6to4) can carry an internal IPv4 address.
var special = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range special {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func ValidURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && len(raw) <= 8192
}

func (f Fetcher) Fetch(ctx context.Context, raw string) ([]byte, error) {
	timeout := f.Timeout
	if timeout <= 0 || timeout > 15*time.Second {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resolver := f.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dial := f.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	// Redirects are followed explicitly. Each hop gets a fresh pinned transport,
	// so neither connection reuse nor a second resolver call can bypass validation.
	for hop := 0; hop < 10; hop++ {
		if !ValidURL(raw) {
			return nil, ErrFetch
		}
		u, _ := url.Parse(raw)
		host := u.Hostname()
		var ips []netip.Addr
		if literal, err := netip.ParseAddr(host); err == nil {
			ips = []netip.Addr{literal}
		} else {
			var err error
			ips, err = resolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, ErrFetch
			}
		}
		if len(ips) == 0 {
			return nil, ErrFetch
		}
		for _, ip := range ips {
			if !Public(ip) {
				return nil, ErrFetch
			}
		}
		port := u.Port()
		if port == "" {
			port = "443"
		}
		address := net.JoinHostPort(ips[0].Unmap().String(), port)
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{ServerName: host, RootCAs: f.roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true,
			DialContext: func(c context.Context, network, _ string) (net.Conn, error) { return dial(c, network, address) },
		}
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, ErrFetch
		}
		response, err := client.Do(request)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, ErrFetch
		}
		if response.StatusCode >= 300 && response.StatusCode <= 399 {
			next, err := response.Location()
			response.Body.Close()
			transport.CloseIdleConnections()
			if err != nil {
				return nil, ErrFetch
			}
			raw = next.String()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, MaxBody+1))
		response.Body.Close()
		transport.CloseIdleConnections()
		// The body read can end without an error once the deadline has cut the
		// connection (seen in CI under -race), so a body read past the deadline
		// is rejected rather than cached as the whole subscription.
		if err != nil || ctx.Err() != nil || response.StatusCode != http.StatusOK || len(body) > MaxBody {
			return nil, ErrFetch
		}
		return body, nil
	}
	return nil, ErrFetch
}
