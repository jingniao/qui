package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixedCoreNames keeps archive fixtures identical on every host platform.
func fixedCoreNames(t *testing.T) {
	t.Helper()
	previous := coreBinaryName
	coreBinaryName = func(component, _ string) string { return component }
	t.Cleanup(func() { coreBinaryName = previous })
}

func TestCoreSourceRedirectPolicy(t *testing.T) {
	fixedCoreNames(t)
	payload := zipBytes(t, []archiveEntry{{name: "xray", data: []byte("binary")}})
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ok.zip":
			w.Write(payload)
		case r.URL.Path == "/plain":
			http.Redirect(w, r, "http://"+r.Host+"/ok.zip", http.StatusFound)
		case r.URL.Path == "/foreign":
			http.Redirect(w, r, "https://evil.example.com/ok.zip", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/hop"):
			http.Redirect(w, r, server.URL+"/hop"+r.URL.Path[len("/hop"):], http.StatusFound)
		}
	}))
	defer server.Close()
	host := "127.0.0.1"
	if parsed, err := url.Parse(server.URL); err == nil {
		host = parsed.Hostname()
	}
	source := testCoreSource(server, host)
	directory := t.TempDir()
	if _, err := source.FetchCore(context.Background(), directory, "xray", server.URL+"/plain", digestOf(payload)); !errors.Is(err, ErrCoreSource) {
		t.Fatalf("insecure redirect accepted: %v", err)
	}
	if _, err := source.FetchCore(context.Background(), directory, "xray", server.URL+"/foreign", digestOf(payload)); !errors.Is(err, ErrCoreSource) {
		t.Fatalf("foreign redirect host accepted: %v", err)
	}
	if _, err := source.FetchCore(context.Background(), directory, "xray", server.URL+"/hop0", digestOf(payload)); !errors.Is(err, ErrCoreSource) {
		t.Fatalf("redirect loop accepted: %v", err)
	}
	if _, err := source.FetchCore(context.Background(), directory, "xray", server.URL+"/ok.zip", digestOf(payload)); err != nil {
		t.Fatalf("direct download refused: %v", err)
	}
}

func TestExtractZipRejectsUnsafeEntries(t *testing.T) {
	cases := []struct {
		name    string
		entries []archiveEntry
	}{
		{"zip slip", []archiveEntry{{name: "../xray", data: []byte("x")}}},
		{"absolute path", []archiveEntry{{name: "/xray", data: []byte("x")}}},
		{"backslash", []archiveEntry{{name: `dir\xray`, data: []byte("x")}}},
		{"symlink", []archiveEntry{{name: "xray", mode: os.ModeSymlink | 0777, data: []byte("x")}}},
		{"duplicate", []archiveEntry{{name: "a/xray", data: []byte("x")}, {name: "a/xray", data: []byte("y")}}},
		{"two candidates", []archiveEntry{{name: "a/xray", data: []byte("x")}, {name: "b/xray", data: []byte("y")}}},
		{"missing binary", []archiveEntry{{name: "a/xray.txt", data: []byte("x")}}},
		{"no entries", nil},
	}
	for _, item := range cases {
		directory := t.TempDir()
		archive := filepath.Join(directory, "archive")
		if err := os.WriteFile(archive, zipBytes(t, item.entries), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := extractCore("Xray-linux-64.zip", "xray", archive, directory); err == nil {
			t.Fatalf("%s accepted", item.name)
		}
		if _, err := os.Stat(filepath.Join(directory, "core")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left an extracted binary", item.name)
		}
	}
}

func TestExtractZipLimitsEntryCount(t *testing.T) {
	entries := make([]archiveEntry, 0, maxCoreArchiveEntries+1)
	for index := 0; index <= maxCoreArchiveEntries; index++ {
		entries = append(entries, archiveEntry{name: "f" + string(rune('a'+index%26)) + string(rune('0'+index/26)), data: []byte("x")})
	}
	directory := t.TempDir()
	archive := filepath.Join(directory, "archive")
	if err := os.WriteFile(archive, zipBytes(t, entries), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractCore("Xray-linux-64.zip", "xray", archive, directory); !errors.Is(err, ErrCoreArchive) {
		t.Fatalf("oversized entry list accepted: %v", err)
	}
}

func TestExtractTarGzRejectsOversizedEntry(t *testing.T) {
	var raw bytes.Buffer
	compressed := gzip.NewWriter(&raw)
	writer := tar.NewWriter(compressed)
	header := &tar.Header{Name: "sing-box", Typeflag: tar.TypeReg, Mode: 0600, Size: MaxCoreBinaryBytes + 1}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archive := filepath.Join(directory, "archive")
	if err := os.WriteFile(archive, raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractCore("sing-box-1.14.3-linux-amd64.tar.gz", "sing-box", archive, directory); !errors.Is(err, ErrCoreArchive) {
		t.Fatalf("oversized entry accepted: %v", err)
	}
}

func TestExtractTarGzNestedAndUnsafeEntries(t *testing.T) {
	fixedCoreNames(t)
	directory := t.TempDir()
	nested := filepath.Join(directory, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(nested, "archive")
	if err := os.WriteFile(archive, tarGzBytes(t, []archiveEntry{{name: "sing-box-1.14.3-linux-amd64/sing-box", data: []byte("binary")}}), 0600); err != nil {
		t.Fatal(err)
	}
	extracted, err := extractCore("sing-box-1.14.3-linux-amd64.tar.gz", "sing-box", archive, nested)
	if err != nil {
		t.Fatalf("nested tar.gz rejected: %v", err)
	}
	if data, err := os.ReadFile(extracted); err != nil || string(data) != "binary" {
		t.Fatalf("nested payload mismatch: %q %v", data, err)
	}
	unsafe := []struct {
		name    string
		entries []archiveEntry
	}{
		{"symlink", []archiveEntry{{name: "sing-box", flag: tar.TypeSymlink, link: "/etc/passwd"}}},
		{"hardlink", []archiveEntry{{name: "sing-box", flag: tar.TypeLink, link: "other"}}},
		{"device", []archiveEntry{{name: "sing-box", flag: tar.TypeChar}}},
		{"traversal", []archiveEntry{{name: "../sing-box", data: []byte("x")}}},
		{"absolute", []archiveEntry{{name: "/sing-box", data: []byte("x")}}},
		{"two candidates", []archiveEntry{{name: "a/sing-box", data: []byte("x")}, {name: "b/sing-box", data: []byte("y")}}},
		{"missing binary", []archiveEntry{{name: "a/sing-box.txt", data: []byte("x")}}},
	}
	for _, item := range unsafe {
		stage := t.TempDir()
		file := filepath.Join(stage, "archive")
		if err := os.WriteFile(file, tarGzBytes(t, item.entries), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := extractCore("sing-box-1.14.3-linux-amd64.tar.gz", "sing-box", file, stage); err == nil {
			t.Fatalf("unsafe tar entry accepted: %s", item.name)
		}
	}
}

// The staged core is executed for its version before installation, so the
// extracted file must carry the owner's execute bit on Unix.
func TestExtractedCoreIsExecutableByOwner(t *testing.T) {
	if coreExecutableSuffix() != "" {
		t.Skip("Windows has no execute permission bits")
	}
	fixedCoreNames(t)
	directory := t.TempDir()
	archive := filepath.Join(directory, "archive")
	if err := os.WriteFile(archive, zipBytes(t, []archiveEntry{{name: "xray", data: []byte("binary")}}), 0600); err != nil {
		t.Fatal(err)
	}
	extracted, err := extractCore("Xray-linux-64.zip", "xray", archive, directory)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0100 == 0 || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("staged core mode %v", info.Mode().Perm())
	}
}
