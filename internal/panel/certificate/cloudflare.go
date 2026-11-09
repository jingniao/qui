package certificate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CloudflareBaseURL is the fixed v4 API root. Tests point a client at their own
// server through NewCloudflare instead.
const CloudflareBaseURL = "https://api.cloudflare.com/client/v4"

// Zone is the Cloudflare zone a certified name belongs to, with the
// authoritative name servers the propagation check queries directly.
type Zone struct {
	ID          string
	Name        string
	NameServers []string
}

// DNS is the Cloudflare subset this module needs. The production value is
// *Cloudflare; tests replace it to observe and fake the record lifecycle.
type DNS interface {
	VerifyToken(ctx context.Context, token string) error
	Zone(ctx context.Context, token, domain string) (Zone, error)
	CreateTXT(ctx context.Context, token, zoneID, name, value string) (string, error)
	DeleteRecord(ctx context.Context, token, zoneID, recordID string) error
	UpsertAddress(ctx context.Context, token, zoneID, name, address string) error
}

// Cloudflare talks to the Cloudflare API with net/http only. One instance uses
// one HTTP client with a 30 second timeout per request.
type Cloudflare struct {
	BaseURL string
	Client  *http.Client
}

// NewCloudflare returns a client for the production API unless a base URL is
// given, which is what tests do.
func NewCloudflare(baseURL string) *Cloudflare {
	if baseURL == "" {
		baseURL = CloudflareBaseURL
	}
	return &Cloudflare{BaseURL: strings.TrimSuffix(baseURL, "/"), Client: &http.Client{Timeout: 30 * time.Second}}
}

// errCloudflareResponse reports a call that completed but was not accepted. It
// carries no upstream text on purpose.
var errCloudflareResponse = errors.New("cloudflare rejected the request")

type cloudflareEnvelope struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
}

// request performs one API call and returns the HTTP status, so the caller can
// map a failure onto the fixed code of the operation it was performing. The
// response body is decoded but never returned or logged.
func (c *Cloudflare) request(ctx context.Context, token, method, path string, body, result any) (int, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		payload = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, payload)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.Client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, err
	}
	var envelope cloudflareEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil || !envelope.Success || response.StatusCode/100 != 2 {
		return response.StatusCode, errCloudflareResponse
	}
	if result != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return response.StatusCode, errCloudflareResponse
		}
	}
	return response.StatusCode, nil
}

// rejected maps a failed call onto the fixed code of the calling operation. A
// cancelled run keeps the cancellation so the worker can record an interruption.
func (c *Cloudflare) rejected(status int, fallback string, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fail(CodeCloudflareTokenInvalid)
	}
	return fail(fallback)
}

// VerifyToken checks a token with Cloudflare before the panel stores it.
func (c *Cloudflare) VerifyToken(ctx context.Context, token string) error {
	var result struct {
		Status string `json:"status"`
	}
	status, err := c.request(ctx, token, http.MethodGet, "/user/tokens/verify", nil, &result)
	if err == nil && result.Status == "active" {
		return nil
	}
	if err == nil {
		return fail(CodeCloudflareTokenInvalid)
	}
	return c.rejected(status, CodeCloudflareAPIFailed, err)
}

// Zone walks the labels of a name until Cloudflare answers with a zone of that
// exact name, so a certificate for a.b.example.com finds example.com.
func (c *Cloudflare) Zone(ctx context.Context, token, domain string) (Zone, error) {
	labels := strings.Split(domain, ".")
	for index := 0; index < len(labels)-1; index++ {
		zone, found, err := c.findZone(ctx, token, strings.Join(labels[index:], "."))
		if err != nil {
			return Zone{}, err
		}
		if found {
			return zone, nil
		}
	}
	return Zone{}, fail(CodeCloudflareZoneNotFound)
}

func (c *Cloudflare) findZone(ctx context.Context, token, name string) (Zone, bool, error) {
	var zones []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		NameServers []string `json:"name_servers"`
	}
	status, err := c.request(ctx, token, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil, &zones)
	if err != nil {
		return Zone{}, false, c.rejected(status, CodeCloudflareAPIFailed, err)
	}
	for _, zone := range zones {
		if zone.Name == name && zone.ID != "" {
			return Zone{ID: zone.ID, Name: zone.Name, NameServers: zone.NameServers}, true, nil
		}
	}
	return Zone{}, false, nil
}

// CreateTXT writes one challenge record and returns its ID for the deferred
// cleanup.
func (c *Cloudflare) CreateTXT(ctx context.Context, token, zoneID, name, value string) (string, error) {
	var record struct {
		ID string `json:"id"`
	}
	body := map[string]any{"type": "TXT", "name": name, "content": value, "ttl": 60}
	status, err := c.request(ctx, token, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", body, &record)
	if err != nil {
		return "", c.rejected(status, CodeCloudflareDNSRecordFailed, err)
	}
	if record.ID == "" {
		return "", fail(CodeCloudflareDNSRecordFailed)
	}
	return record.ID, nil
}

// DeleteRecord removes a record the run created. A record that is already gone
// is not a failure: the cleanup must not turn a completed issuance into an
// error.
func (c *Cloudflare) DeleteRecord(ctx context.Context, token, zoneID, recordID string) error {
	status, err := c.request(ctx, token, http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), nil, nil)
	if err == nil || status == http.StatusNotFound {
		return nil
	}
	return c.rejected(status, CodeCloudflareDNSRecordFailed, err)
}

// UpsertAddress points one name at the node's public address. IPv4 becomes an A
// record and IPv6 an AAAA record, both unproxied with automatic TTL: a proxied
// record hides the node, and Cloudflare's proxy does not carry UDP, which
// Hysteria2 and TUIC need.
func (c *Cloudflare) UpsertAddress(ctx context.Context, token, zoneID, name, address string) error {
	ip := net.ParseIP(address)
	if ip == nil {
		return fail(CodeDNSNodeAddressMissing)
	}
	recordType := "A"
	if ip.To4() == nil {
		recordType = "AAAA"
	}
	var existing []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records?type=" + recordType + "&name=" + url.QueryEscape(name)
	if status, err := c.request(ctx, token, http.MethodGet, path, nil, &existing); err != nil {
		return c.rejected(status, CodeCloudflareDNSRecordFailed, err)
	}
	body := map[string]any{"type": recordType, "name": name, "content": address, "ttl": 1, "proxied": false}
	for _, record := range existing {
		if record.Content == address {
			return nil
		}
		if status, err := c.request(ctx, token, http.MethodPut, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(record.ID), body, nil); err != nil {
			return c.rejected(status, CodeCloudflareDNSRecordFailed, err)
		}
		return nil
	}
	if status, err := c.request(ctx, token, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", body, nil); err != nil {
		return c.rejected(status, CodeCloudflareDNSRecordFailed, err)
	}
	return nil
}
