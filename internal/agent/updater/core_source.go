package updater

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"proxycontrol/pkg/contracts/release"
)

const (
	// MaxCoreArchiveBytes caps one downloaded release archive.
	MaxCoreArchiveBytes int64 = 128 << 20
	// MaxCoreBinaryBytes caps the single extracted executable.
	MaxCoreBinaryBytes    int64 = 256 << 20
	maxCoreArchiveEntries       = 64
	maxCoreRedirects            = 3
)

var (
	ErrCoreSource   = errors.New("core release download rejected")
	ErrCoreChecksum = errors.New("core release archive checksum mismatch")
	ErrCoreArchive  = errors.New("core release archive rejected")
)

// CoreSource downloads one official release asset, verifies the archive
// SHA-256 while streaming and only then extracts the single expected
// executable. The URL policy and TLS roots are injectable so tests can serve
// an archive locally; production defaults accept official GitHub assets only.
type CoreSource struct {
	AllowURL          func(component, value string) (*url.URL, string, error)
	AllowRedirectHost func(host string) bool
	Roots             *x509.CertPool
}

func NewCoreSource() *CoreSource {
	return &CoreSource{AllowURL: release.CoreAssetURL, AllowRedirectHost: release.CoreRedirectHost}
}

func (s *CoreSource) allowURL(component, value string) (*url.URL, string, error) {
	if s.AllowURL == nil {
		return release.CoreAssetURL(component, value)
	}
	return s.AllowURL(component, value)
}

func (s *CoreSource) allowRedirect(host string) bool {
	if s.AllowRedirectHost == nil {
		return release.CoreRedirectHost(host)
	}
	return s.AllowRedirectHost(host)
}

func (s *CoreSource) client() *http.Client {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if s.Roots != nil {
		config.RootCAs = s.Roots.Clone()
	}
	transport := &http.Transport{TLSClientConfig: config, DisableCompression: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 16 << 10, MaxIdleConns: 2, MaxConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: transport, Timeout: 10 * time.Minute, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > maxCoreRedirects || request.URL.Scheme != "https" || request.URL.User != nil || !s.allowRedirect(request.URL.Hostname()) {
			return ErrCoreSource
		}
		// A redirect never carries credentials, cookies or a re-encoded body.
		request.Header.Del("Cookie")
		request.Header.Del("Authorization")
		request.Header.Del("Referer")
		request.Header.Set("Accept-Encoding", "identity")
		return nil
	}}
}

// FetchCore downloads the asset, verifies the expected archive digest before
// extraction and returns the path of the single extracted executable inside a
// fresh staging directory. The caller removes the staging directory.
func (s *CoreSource) FetchCore(ctx context.Context, directory, component, assetURL, digest string) (string, error) {
	u, _, err := s.allowURL(component, assetURL)
	if err != nil {
		return "", errors.Join(ErrCoreSource, err)
	}
	if release.ArchiveKind(path.Base(u.Path)) == "" {
		return "", ErrCoreSource
	}
	staging, err := stagingDirectory(directory)
	if err != nil {
		return "", err
	}
	archive, err := s.download(ctx, u, digest, filepath.Join(staging, "archive"))
	if err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	defer os.Remove(archive)
	extracted, err := extractCore(path.Base(u.Path), component, archive, staging)
	if err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	return extracted, nil
}

func stagingDirectory(directory string) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	staging := filepath.Join(directory, ".core-staging-"+hex.EncodeToString(random))
	if err := os.Mkdir(staging, 0700); err != nil {
		return "", err
	}
	return staging, nil
}

// download streams the archive to disk while hashing it. A digest mismatch is
// reported before any extraction happens.
func (s *CoreSource) download(ctx context.Context, u *url.URL, digest, destination string) (result string, resultErr error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", ErrCoreSource
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := s.client().Do(request)
	if err != nil {
		return "", errors.Join(ErrCoreSource, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" || response.ContentLength > MaxCoreArchiveBytes {
		return "", ErrCoreSource
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		file.Close()
		if resultErr != nil {
			os.Remove(destination)
		}
	}()
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx: ctx, reader: response.Body}, MaxCoreArchiveBytes+1))
	if err != nil {
		return "", err
	}
	if count < 1 || count > MaxCoreArchiveBytes {
		return "", ErrCoreSource
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return "", ErrCoreChecksum
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return destination, nil
}

// extractCore and safeEntryName live in core_archive.go.
