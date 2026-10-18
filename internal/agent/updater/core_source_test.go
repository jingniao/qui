package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/pkg/contracts/release"
)

type archiveEntry struct {
	name  string
	data  []byte
	mode  os.FileMode
	link  string
	flag  byte
	size  int64
	noReg bool
}

func zipBytes(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		if entry.noReg {
			header.SetMode(os.ModeSymlink | 0777)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func tarGzBytes(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		flag := entry.flag
		if flag == 0 {
			flag = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Typeflag: flag, Mode: 0600, Size: entry.size}
		if header.Size == 0 {
			header.Size = int64(len(entry.data))
		}
		if entry.link != "" {
			header.Linkname = entry.link
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// testCoreSource accepts the local httptest origin so the download path can be
// exercised; the production allow-list is covered by the release package.
func testCoreSource(server *httptest.Server, allowedHosts ...string) *CoreSource {
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	hosts := map[string]bool{}
	for _, host := range allowedHosts {
		hosts[host] = true
	}
	return &CoreSource{
		AllowURL: func(component, value string) (*url.URL, string, error) {
			u, err := url.Parse(value)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" {
				return nil, "", release.ErrSourceURL
			}
			return u, "v1", nil
		},
		AllowRedirectHost: func(host string) bool { return hosts[host] },
		Roots:             pool,
	}
}

func TestCoreSourceVerifiesArchiveBeforeExtracting(t *testing.T) {
	fixedCoreNames(t)
	payload := zipBytes(t, []archiveEntry{{name: "Xray-linux-64/xray", data: []byte("fake-xray-binary")}})
	var cookie string
	var encoding string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, encoding = r.Header.Get("Cookie"), r.Header.Get("Accept-Encoding")
		w.Write(payload)
	}))
	defer server.Close()
	source := testCoreSource(server)
	directory := t.TempDir()
	path, err := source.FetchCore(context.Background(), directory, "xray", server.URL+"/Xray-linux-64.zip", digestOf(payload))
	if err != nil {
		t.Fatalf("verified archive rejected: %v", err)
	}
	extracted, err := os.ReadFile(path)
	if err != nil || string(extracted) != "fake-xray-binary" {
		t.Fatalf("extracted payload mismatch: %q %v", extracted, err)
	}
	if cookie != "" || encoding != "identity" {
		t.Fatalf("request carried credentials or compression: %q %q", cookie, encoding)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "core"+coreExecutableSuffix() {
		t.Fatalf("staging directory kept unexpected entries: %v %v", entries, err)
	}
}

func TestCoreSourceChecksumMismatchLeavesNothingBehind(t *testing.T) {
	payload := zipBytes(t, []archiveEntry{{name: "xray", data: []byte("fake-xray-binary")}})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer server.Close()
	directory := t.TempDir()
	if _, err := testCoreSource(server).FetchCore(context.Background(), directory, "xray", server.URL+"/Xray-linux-64.zip", strings.Repeat("ab", 32)); !errors.Is(err, ErrCoreChecksum) {
		t.Fatalf("checksum mismatch not reported: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging left behind after mismatch: %v %v", entries, err)
	}
}

func TestCoreSourceRejectsUnsupportedArchiveAndURL(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	directory := t.TempDir()
	if _, err := testCoreSource(server).FetchCore(context.Background(), directory, "xray", server.URL+"/Xray-linux-64.txt", digestOf(nil)); !errors.Is(err, ErrCoreSource) {
		t.Fatalf("non-archive URL accepted: %v", err)
	}
	if _, err := testCoreSource(server).FetchCore(context.Background(), directory, "xray", "https://evil.example.com/Xray-linux-64.zip", digestOf(nil)); !errors.Is(err, ErrCoreSource) {
		t.Fatalf("untrusted URL accepted: %v", err)
	}
}
