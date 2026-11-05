package importer

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type fakeResolver struct {
	answers [][]netip.Addr
	calls   int
}

func (r *fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	i := r.calls
	r.calls++
	if i >= len(r.answers) {
		i = len(r.answers) - 1
	}
	return r.answers[i], nil
}
func addresses(values ...string) []netip.Addr {
	a := []netip.Addr{}
	for _, v := range values {
		a = append(a, netip.MustParseAddr(v))
	}
	return a
}

func fetchFixture(t *testing.T, h http.HandlerFunc) (Fetcher, *fakeResolver, *int) {
	t.Helper()
	server := httptest.NewTLSServer(h)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	r := &fakeResolver{answers: [][]netip.Addr{addresses("8.8.8.8")}}
	calls := 0
	return Fetcher{Resolver: r, roots: roots, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		calls++
		if address != "8.8.8.8:443" {
			t.Errorf("not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}, r, &calls
}

// AU5.1 checks all answers, not just the first address, including mapped v4.
func TestSSRFAddressTable(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.1.1", "224.0.0.1", "0.0.0.0", "100.64.0.1", "100.127.255.255", "::1", "::", "fc00::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:100.64.1.1",
		"0.1.2.3", "192.0.0.8", "198.18.0.1", "240.0.0.1", "64:ff9b::a00:1", "64:ff9b:1::1", "100::1", "2001:db8::1", "2002:a00:1::1"} {
		t.Run(ip, func(t *testing.T) {
			r := &fakeResolver{answers: [][]netip.Addr{addresses("8.8.8.8", ip)}}
			dialed := false
			f := Fetcher{Resolver: r, DialContext: func(context.Context, string, string) (net.Conn, error) { dialed = true; return nil, ErrFetch }}
			if _, err := f.Fetch(context.Background(), "https://example.com"); err == nil || dialed || r.calls != 1 {
				t.Fatal("unsafe address reached dial")
			}
		})
	}
	for _, raw := range []string{"http://example.com", "https://127.0.0.1", "https://[::ffff:10.0.0.1]", "https://user:pass@example.com", "https://example.com/#fragment"} {
		f := Fetcher{}
		if _, err := f.Fetch(context.Background(), raw); err == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700:4700::1111", "100.128.0.1", "::ffff:8.8.8.8"} {
		if !Public(netip.MustParseAddr(ip)) {
			t.Fatal("public address rejected")
		}
	}
}

func TestSSRFPinsDNSAndRevalidatesRedirects(t *testing.T) {
	f, r, dials := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "example.com" {
			t.Error("wrong SNI")
		}
		fmt.Fprint(w, "ok")
	})
	r.answers = [][]netip.Addr{addresses("8.8.8.8"), addresses("127.0.0.1")}
	if b, err := f.Fetch(context.Background(), "https://example.com"); err != nil || string(b) != "ok" || r.calls != 1 || *dials != 1 {
		t.Fatalf("pinned fetch: %v, calls=%d dials=%d", err, r.calls, *dials)
	}
	if _, err := f.Fetch(context.Background(), "https://example.com"); err == nil || r.calls != 2 || *dials != 1 {
		t.Fatal("changed DNS answer accepted")
	}
	for _, location := range []string{"https://127.0.0.1/private", "https://example.com/next", "http://example.com/next"} {
		t.Run(location, func(t *testing.T) {
			f, r, d := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, location, 302) })
			r.answers = [][]netip.Addr{addresses("8.8.8.8"), addresses("192.168.1.1")}
			if _, err := f.Fetch(context.Background(), "https://example.com"); err == nil || *d != 1 {
				t.Fatal("redirect escaped validation")
			}
		})
	}
}

func TestFetchSizeAndTotalTimeout(t *testing.T) {
	for _, size := range []int{MaxBody, MaxBody + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f, _, _ := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("a", size)) })
			body, err := f.Fetch(context.Background(), "https://example.com")
			if (err == nil) != (size == MaxBody) || err == nil && len(body) != size {
				t.Fatal("body cap failed")
			}
		})
	}
	f, _, _ := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		// Without the total timeout the body arrives after 10 s and Fetch
		// succeeds; the 5 s bound below leaves room for a slow -race runner.
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
		fmt.Fprint(w, "late")
	})
	f.Timeout = 30 * time.Millisecond
	start := time.Now()
	if _, err := f.Fetch(context.Background(), "https://example.com"); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("total timeout not enforced: err=%v after %s", err, time.Since(start))
	}
}

// Every fetch failure is typed: the Owner gets a safe category instead of a
// generic refusal, and no error text carries the URL, the redirect target or a
// response body.
func TestFetchErrorCategories(t *testing.T) {
	code := func(t *testing.T, err error) *FetchError {
		t.Helper()
		var typed *FetchError
		if !errors.As(err, &typed) || !errors.Is(err, ErrFetch) {
			t.Fatalf("untyped fetch error: %v", err)
		}
		return typed
	}
	t.Run("dns", func(t *testing.T) {
		f := Fetcher{Resolver: failingResolver{}, DialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Error("dialled after a failed lookup")
			return nil, ErrFetch
		}}
		typed := code(t, fetchErr(f, "https://example.com"))
		if typed.Stage != stageDNS || typed.Code != "source_dns_failed" {
			t.Fatalf("dns: %+v", typed)
		}
	})
	t.Run("address", func(t *testing.T) {
		f, _, _ := fetchFixture(t, func(http.ResponseWriter, *http.Request) {})
		r := &fakeResolver{answers: [][]netip.Addr{addresses("10.0.0.5")}}
		f.Resolver = r
		typed := code(t, fetchErr(f, "https://example.com"))
		if typed.Stage != stageAddress || typed.Code != "source_address_rejected" {
			t.Fatalf("address: %+v", typed)
		}
	})
	t.Run("redirect scheme", func(t *testing.T) {
		f, _, _ := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com/next", 302) })
		typed := code(t, fetchErr(f, "https://example.com"))
		if typed.Stage != stageRedirect || typed.Code != "source_redirect_rejected" {
			t.Fatalf("redirect: %+v", typed)
		}
	})
	t.Run("tls", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
		defer server.Close()
		f := Fetcher{Resolver: &fakeResolver{answers: [][]netip.Addr{addresses("8.8.8.8")}}, roots: x509.NewCertPool(),
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
		typed := code(t, fetchErr(f, "https://example.com"))
		if typed.Stage != stageTLS || typed.Code != "source_tls_failed" {
			t.Fatalf("tls: %+v", typed)
		}
	})
	t.Run("connect", func(t *testing.T) {
		f := Fetcher{Resolver: &fakeResolver{answers: [][]netip.Addr{addresses("8.8.8.8")}}, DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		}}
		typed := code(t, fetchErr(f, "https://example.com"))
		if typed.Stage != stageConnect || typed.Code != "source_connect_failed" {
			t.Fatalf("connect: %+v", typed)
		}
	})
	t.Run("http status", func(t *testing.T) {
		f, _, _ := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
		typed := code(t, fetchErr(f, "https://example.com/fixture?token=private-fixture"))
		if typed.Stage != stageHTTP || typed.Code != "source_http_status" || typed.Status != http.StatusForbidden {
			t.Fatalf("http: %+v", typed)
		}
		if message := typed.Message(); !strings.Contains(message, "403") || strings.Contains(message, "private-fixture") {
			t.Fatalf("http message: %q", message)
		}
	})
	t.Run("size", func(t *testing.T) {
		f, _, _ := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("a", MaxBody+1)) })
		typed := code(t, fetchErr(f, "https://example.com"))
		if typed.Stage != stageSize || typed.Code != "source_too_large" {
			t.Fatalf("size: %+v", typed)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		f, _, _ := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		})
		f.Timeout = 20 * time.Millisecond
		typed := code(t, fetchErr(f, "https://example.com"))
		if typed.Stage != stageDeadline || typed.Code != "source_timeout" {
			t.Fatalf("deadline: %+v", typed)
		}
	})
}

func fetchErr(f Fetcher, raw string) error {
	_, err := f.Fetch(context.Background(), raw)
	return err
}

type failingResolver struct{}

func (failingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, &net.DNSError{Err: "no such host", Name: "example.com", IsNotFound: true}
}
