package updater

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"proxycontrol/pkg/contracts/release"
)

var ErrDownload = errors.New("release catalogue download rejected")

// CatalogueURL accepts only a local configured HTTPS directory. Neither signed
// manifests nor control messages may introduce URLs, redirects or credentials.
func CatalogueURL(value string) (*url.URL, error) { return release.CatalogueURL(value) }

// Downloader has one approved origin and its own transport. It never forwards
// panel cookies, node certificates, signing keys or proxy environment settings.
type Downloader struct {
	base      *url.URL
	client    *http.Client
	transport *http.Transport
}

func NewDownloader(catalogue string, trust *tls.Config) (*Downloader, error) {
	base, err := CatalogueURL(catalogue)
	if err != nil {
		return nil, err
	}
	c := &tls.Config{MinVersion: tls.VersionTLS13}
	if trust != nil {
		if trust.InsecureSkipVerify || len(trust.Certificates) != 0 || trust.GetClientCertificate != nil || trust.VerifyPeerCertificate != nil || trust.VerifyConnection != nil {
			return nil, ErrDownload
		}
		c = trust.Clone()
		if c.RootCAs != nil {
			c.RootCAs = c.RootCAs.Clone()
		}
		c.MinVersion = max(c.MinVersion, tls.VersionTLS13)
		// The catalogue hostname, rather than a caller-provided override, must
		// match the authenticated peer certificate.
		c.ServerName = ""
	}
	transport := &http.Transport{TLSClientConfig: c, DisableCompression: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 16 << 10, MaxIdleConns: 2, MaxConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrDownload }}
	return &Downloader{base: base, client: client, transport: transport}, nil
}
func (d *Downloader) Close() { d.transport.CloseIdleConnections() }

func (d *Downloader) get(ctx context.Context, path string, maximum int64, exact bool) (*http.Response, error) {
	u := *d.base
	u.Path += path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrDownload
	}
	req.Header.Set("Accept-Encoding", "identity")
	response, err := d.client.Do(req)
	if err != nil {
		return nil, errors.Join(ErrDownload, err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" || response.ContentLength > maximum || exact && response.ContentLength >= 0 && response.ContentLength != maximum {
		response.Body.Close()
		return nil, ErrDownload
	}
	return response, nil
}

// Fetch returns a verified non-executable candidate. Acceptance is committed
// only after the streamed artifact has passed size, SHA256 and native format
// checks. History retains replay protection across restart and failed installs.
func (d *Downloader) Fetch(ctx context.Context, directory string, sequence int64, policy release.Policy, history *History) (Candidate, error) {
	return d.fetch(ctx, directory, sequence, policy, history, nil)
}

// FetchExpected binds catalogue contents to the central immutable command
// before either streaming bytes or reserving the local acceptance sequence.
func (d *Downloader) FetchExpected(ctx context.Context, directory string, expected release.Manifest, policy release.Policy, history *History) (Candidate, error) {
	return d.fetch(ctx, directory, expected.Sequence, policy, history, &expected)
}
func (d *Downloader) fetch(ctx context.Context, directory string, sequence int64, policy release.Policy, history *History, expected *release.Manifest) (Candidate, error) {
	if history == nil || !release.Component(policy.Component) || sequence < 1 || policy.OS != "linux" && policy.OS != "windows" || policy.Architecture != "amd64" && policy.Architecture != "arm64" {
		return Candidate{}, ErrDownload
	}
	if err := history.CheckSequence(policy.Component, sequence); err != nil {
		return Candidate{}, err
	}
	path := fmt.Sprintf("%s/%s/%s/%d/manifest.json", policy.Component, policy.OS, policy.Architecture, sequence)
	response, err := d.get(ctx, path, release.MaxManifestBytes, false)
	if err != nil {
		return Candidate{}, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, release.MaxManifestBytes+1))
	response.Body.Close()
	if err != nil {
		return Candidate{}, err
	}
	// A response can still arrive, and its body read end without an error,
	// after the deadline cancelled the request (seen in CI under -race), so
	// that is reported as the cancellation rather than as a bad manifest.
	if err := ctx.Err(); err != nil {
		return Candidate{}, errors.Join(ErrDownload, err)
	}
	signed, err := release.Decode(data)
	if err != nil {
		return Candidate{}, err
	}
	if signed.Manifest.Sequence != sequence {
		return Candidate{}, ErrDownload
	}
	if expected != nil && signed.Manifest != *expected {
		return Candidate{}, ErrDownload
	}
	policy.Now = time.Now().UTC()
	if err := history.Check(signed, policy); err != nil {
		return Candidate{}, err
	}
	response, err = d.get(ctx, "artifacts/"+signed.Manifest.SHA256+".bin", signed.Manifest.Bytes, true)
	if err != nil {
		return Candidate{}, err
	}
	defer response.Body.Close()
	candidate, err := Stage(ctx, directory, signed, policy, response.Body)
	if err != nil {
		return Candidate{}, err
	}
	policy.Now = time.Now().UTC()
	if err := history.Accept(ctx, signed, policy); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}
