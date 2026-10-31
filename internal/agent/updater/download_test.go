package updater

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func trustedDownloader(t *testing.T, server *httptest.Server) *Downloader {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	d, err := NewDownloader(server.URL+"/releases", &tls.Config{RootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d
}
func TestHTTPSDownloadStagesRealExecutableAndPreservesAcceptanceAfterRestart(t *testing.T) {
	options, signed, policy, _ := historyFixture(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(signed)
	var manifests, artifacts atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Accept-Encoding") != "identity" || len(r.TLS.PeerCertificates) != 0 {
			t.Error("unexpected request credentials or encoding")
		}
		switch r.URL.Path {
		case "/releases/" + policy.Component + "/" + policy.OS + "/" + policy.Architecture + "/1/manifest.json":
			manifests.Add(1)
			w.Write(manifest)
		case "/releases/artifacts/" + signed.Manifest.SHA256 + ".bin":
			artifacts.Add(1)
			w.Write(artifact)
		default:
			t.Error("unapproved download path", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := trustedDownloader(t, server)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "candidates")
	candidate, err := d.Fetch(context.Background(), directory, 1, policy, h)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.SHA256 != signed.Manifest.SHA256 || h.head.Number != 1 {
		t.Fatal("download not durably accepted")
	}
	stored, err := os.ReadFile(candidate.Path)
	if err != nil || !bytes.Equal(stored, artifact) {
		t.Fatal("downloaded bytes differ", err)
	}
	h.Close()
	h, err = OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := d.Fetch(context.Background(), directory, 1, policy, h); err != nil {
		t.Fatal(err)
	}
	if h.head.Number != 1 || manifests.Load() != 2 || artifacts.Load() != 2 {
		t.Fatal("retry changed history")
	}
}
func TestDownloadRejectsRedirectEncodingUnsignedAndBadArtifactWithoutAcceptance(t *testing.T) {
	for _, fault := range []string{"redirect", "gzip", "unsigned", "bad-bytes", "extra-bytes", "missing", "large-manifest"} {
		t.Run(fault, func(t *testing.T) {
			options, signed, policy, _ := historyFixture(t)
			binary, _ := os.Executable()
			artifact, err := os.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "unsigned" {
				signed.Signature = strings.Repeat("A", 88)
			}
			manifest, _ := json.Marshal(signed)
			var artifactRequests, redirectRequests atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/elsewhere" {
					redirectRequests.Add(1)
					w.Write(manifest)
					return
				}
				if strings.HasSuffix(r.URL.Path, "manifest.json") {
					switch fault {
					case "redirect":
						http.Redirect(w, r, "/elsewhere", http.StatusFound)
						return
					case "gzip":
						w.Header().Set("Content-Encoding", "gzip")
					case "missing":
						http.NotFound(w, r)
						return
					case "large-manifest":
						w.Write(bytes.Repeat([]byte(" "), 20000))
						return
					}
					w.Write(manifest)
					return
				}
				artifactRequests.Add(1)
				if fault == "bad-bytes" {
					artifact = append([]byte("CHANGED"), artifact[7:]...)
				}
				if fault == "extra-bytes" {
					w.(http.Flusher).Flush()
					artifact = append(artifact, 0)
				}
				w.Write(artifact)
			}))
			defer server.Close()
			d := trustedDownloader(t, server)
			h, err := CreateHistory(options)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			if _, err := d.Fetch(context.Background(), t.TempDir(), 1, policy, h); err == nil {
				t.Fatal("bad release accepted")
			}
			if h.head.Number != 0 || redirectRequests.Load() != 0 {
				t.Fatal("untrusted response committed or redirect followed")
			}
			if fault != "bad-bytes" && fault != "extra-bytes" && artifactRequests.Load() != 0 {
				t.Fatal("artifact downloaded before manifest validation")
			}
		})
	}
}
func TestCatalogueTrustAndCancellation(t *testing.T) {
	for _, value := range []string{"http://example.com/releases", "https://user:password@example.com", "https://example.com/releases?token=secret", "https://example.com/releases#fragment", "https://example.com/a/../b", "https://example.com/%2e%2e/b", "https://example.com:0/releases"} {
		if _, err := NewDownloader(value, nil); err == nil {
			t.Fatal("unsafe catalogue accepted", value)
		}
	}
	if _, err := NewDownloader("https://example.com", &tls.Config{InsecureSkipVerify: true}); err == nil {
		t.Fatal("unauthenticated TLS accepted")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	d := trustedDownloader(t, server)
	options, _, policy, _ := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := d.Fetch(ctx, t.TempDir(), 1, policy, h); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("download cancellation lost", err)
	}
	if h.head.Number != 0 {
		t.Fatal("cancellation advanced sequence")
	}
}

// The CI failure of the test above: the deadline cancelled the request, yet an
// empty 200 response was returned and read without an error.
func TestFetchReportsCancellationWhenAResponseRacesTheDeadline(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	d := trustedDownloader(t, server)
	d.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})
	options, _, policy, _ := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := d.Fetch(ctx, t.TempDir(), 1, policy, h); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("download cancellation lost", err)
	}
	if h.head.Number != 0 {
		t.Fatal("cancellation advanced sequence")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExpectedCommandManifestIsCheckedBeforeArtifactOrAcceptance(t *testing.T) {
	options, signed, policy, _ := historyFixture(t)
	manifest, _ := json.Marshal(signed)
	var artifactRequests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			w.Write(manifest)
		} else {
			artifactRequests.Add(1)
			w.Write([]byte("not requested"))
		}
	}))
	defer server.Close()
	d := trustedDownloader(t, server)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	expected := signed.Manifest
	expected.Version = "different-registered-version"
	if _, err := d.FetchExpected(context.Background(), t.TempDir(), expected, policy, h); err == nil {
		t.Fatal("different catalogue manifest accepted for immutable command")
	}
	if artifactRequests.Load() != 0 || h.head.Number != 0 {
		t.Fatal("mismatch streamed bytes or advanced sequence")
	}
}
