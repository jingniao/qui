// Package certificate stores the TLS certificates the panel obtains itself
// through the ACME DNS-01 challenge (v1.4.7 section 4, phase D1).
//
// The panel talks to Cloudflare and to the CA directly: no Cloudflare token or
// private key is ever written into a node configuration, and no Agent learns
// that a certificate exists until phase D2 references one from an inbound. What
// this module owns is one row per certificate request, the vault-sealed
// certificate material, the Cloudflare token, the ACME account key, and one
// worker that issues and renews one certificate at a time.
package certificate

import (
	"encoding/json"
	"errors"
	"net"
	"net/mail"
	"strings"
	"time"

	"proxycontrol/pkg/contracts/netaddr"
)

// Certificate statuses. pending is a request nobody has attempted yet, issuing
// is the single run in flight, valid means the stored material authenticates the
// names, and failed is a row the backoff will retry.
const (
	StatusPending = "pending"
	StatusIssuing = "issuing"
	StatusValid   = "valid"
	StatusFailed  = "failed"
)

// Fixed failure codes. Only these reach the database and the API; the Chinese
// text that goes with each one lives in errorText below, so an upstream body,
// URL or token can never be shown to a browser.
const (
	CodeCloudflareNotConfigured   = "cloudflare_not_configured"
	CodeCloudflareTokenInvalid    = "cloudflare_token_invalid"
	CodeCloudflareZoneNotFound    = "cloudflare_zone_not_found"
	CodeCloudflareAPIFailed       = "cloudflare_api_failed"
	CodeCloudflareDNSRecordFailed = "cloudflare_dns_record_failed"
	CodeDNSNodeAddressMissing     = "dns_node_address_missing"
	CodeDNSPropagationTimeout     = "dns_propagation_timeout"
	CodeACMEAccountFailed         = "acme_account_failed"
	CodeACMEOrderFailed           = "acme_order_failed"
	CodeACMEAuthorizationFailed   = "acme_authorization_failed"
	CodeACMEFinalizeFailed        = "acme_finalize_failed"
	CodeACMERateLimited           = "acme_rate_limited"
	CodeInterrupted               = "interrupted"
	CodeInternal                  = "internal"
)

var errorText = map[string]string{
	CodeCloudflareNotConfigured:   "尚未配置 Cloudflare API Token",
	CodeCloudflareTokenInvalid:    "Cloudflare API Token 无效或权限不足",
	CodeCloudflareZoneNotFound:    "在 Cloudflare 中找不到该域名的托管区域",
	CodeCloudflareAPIFailed:       "调用 Cloudflare 接口失败",
	CodeCloudflareDNSRecordFailed: "写入 Cloudflare DNS 记录失败",
	CodeDNSNodeAddressMissing:     "所选节点没有公网地址，无法写入 DNS 记录",
	CodeDNSPropagationTimeout:     "等待 DNS 生效超时",
	CodeACMEAccountFailed:         "创建 ACME 账户失败",
	CodeACMEOrderFailed:           "创建证书订单失败",
	CodeACMEAuthorizationFailed:   "域名验证未通过",
	CodeACMEFinalizeFailed:        "签发证书失败",
	CodeACMERateLimited:           "CA 限流，请稍后重试",
	CodeInterrupted:               "证书任务被中断",
	CodeInternal:                  "内部错误",
}

// ErrorText returns the fixed Chinese explanation of a stored code. An empty or
// unknown code has none, so the page can show its own "never failed" state.
func ErrorText(code string) string { return errorText[code] }

// KnownErrorCode reports whether a code is one this build stores. Backup
// validation uses it to refuse a row this panel could never have written.
func KnownErrorCode(code string) bool {
	_, ok := errorText[code]
	return ok
}

// maxDomains is the number of names one certificate may cover. It matches what
// the CA accepts in a single order in practice and keeps the DNS-01 fan-out of
// one job small enough that a serialized worker finishes it well inside the
// responsiveness of the Owner's "申请" button.
const maxDomains = 10

// backoff is the retry delay after the 1st, 2nd and every later failed attempt.
var backoff = []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour}

// NextAttempt is the timestamp (UTC, RFC 3339 with nanoseconds) the scheduler
// may retry at after n failed attempts, n starting at 1.
func NextAttempt(now time.Time, attempts int) string {
	step := backoff[len(backoff)-1]
	if attempts >= 1 && attempts <= len(backoff) {
		step = backoff[attempts-1]
	}
	return Stamp(now.Add(step))
}

// Stamp formats a time the way every other panel table stores one.
func Stamp(now time.Time) string { return now.UTC().Format(time.RFC3339Nano) }

// parseStamp reads a stored UTC timestamp; an empty column is not a timestamp.
func parseStamp(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil && value != ""
}

// Material is the issued certificate bundle plus its private key. It is what the
// vault seals into certificates.encrypted_material and what phase D2 renders
// into an inbound.
type Material struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

// Certificate is one stored row without its material.
type Certificate struct {
	ID            string
	Domains       []string
	Status        string
	NotBefore     string
	NotAfter      string
	LastErrorCode string
	LastAttemptAt string
	NextAttemptAt string
	Attempts      int
	AutoDNS       bool
	DNSNodeID     string
	// HasMaterial reports whether an earlier issuance succeeded, which is what
	// keeps a certificate valid while a renewal fails.
	HasMaterial bool
	CreatedAt   string
	UpdatedAt   string
}

// ExpiresAt is the certificate's not-after time, when one is stored.
func (c Certificate) ExpiresAt() (time.Time, bool) { return parseStamp(c.NotAfter) }

// Usage is one inbound that references a certificate. Phase D2 fills it; the
// foreign key exists from 0137 so the reference survives the panel restart.
type Usage struct {
	InboundID string `json:"inboundId"`
	Name      string `json:"name"`
	NodeID    string `json:"nodeId"`
}

// View is the API shape of one certificate. It never carries material.
type View struct {
	ID            string   `json:"id"`
	Domains       []string `json:"domains"`
	Status        string   `json:"status"`
	NotBefore     string   `json:"notBefore"`
	NotAfter      string   `json:"notAfter"`
	LastErrorCode string   `json:"lastErrorCode"`
	LastErrorText string   `json:"lastErrorText"`
	LastAttemptAt string   `json:"lastAttemptAt"`
	NextAttemptAt string   `json:"nextAttemptAt"`
	AutoDNS       bool     `json:"autoDns"`
	DNSNodeID     string   `json:"dnsNodeId"`
	UsedBy        []Usage  `json:"usedBy"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

// NewView renders a row for the API, defaulting usedBy to an empty array so the
// page never has to tell null from "referenced by nobody".
func NewView(c Certificate, usedBy []Usage) View {
	if usedBy == nil {
		usedBy = []Usage{}
	}
	return View{ID: c.ID, Domains: c.Domains, Status: c.Status, NotBefore: c.NotBefore, NotAfter: c.NotAfter,
		LastErrorCode: c.LastErrorCode, LastErrorText: ErrorText(c.LastErrorCode), LastAttemptAt: c.LastAttemptAt,
		NextAttemptAt: c.NextAttemptAt, AutoDNS: c.AutoDNS, DNSNodeID: c.DNSNodeID, UsedBy: usedBy,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

// CreateInput is the POST /certificates body.
type CreateInput struct {
	Domains   []string `json:"domains"`
	AutoDNS   bool     `json:"autoDns"`
	DNSNodeID string   `json:"dnsNodeId"`
}

// NormalizeDomains lower-cases, de-duplicates and validates a requested name
// list. A name is either a DNS name the panel can dial or the same with one
// leading "*." wildcard label; IP literals are refused because DNS-01 authorizes
// names, and a name without a dot can never belong to a public zone.
func NormalizeDomains(domains []string) ([]string, error) {
	if len(domains) == 0 || len(domains) > maxDomains {
		return nil, ErrDomains
	}
	result := make([]string, 0, len(domains))
	seen := map[string]bool{}
	for _, raw := range domains {
		name, err := normalizeDomain(raw)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, ErrDomains
		}
		seen[name] = true
		result = append(result, name)
	}
	return result, nil
}

// normalizeDomain accepts "host.example.com" and "*.host.example.com" only. The
// wildcard is a single leading label, never a bare "*", never inside the name.
func normalizeDomain(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.TrimSuffix(name, ".")
	base := strings.TrimPrefix(name, "*.")
	if base == "" || strings.Contains(base, "*") || strings.ContainsFunc(name, func(value rune) bool { return value <= ' ' || value == 0x7f }) {
		return "", ErrDomains
	}
	if net.ParseIP(base) != nil || !netaddr.ValidHostName(base) || !strings.Contains(base, ".") {
		return "", ErrDomains
	}
	return name, nil
}

// ParseDomains decodes the stored JSON array. Rows the live panel wrote always
// satisfy NormalizeDomains; anything else is treated as archive damage.
func ParseDomains(raw string) ([]string, error) {
	var domains []string
	if err := json.Unmarshal([]byte(raw), &domains); err != nil {
		return nil, ErrDomains
	}
	return NormalizeDomains(domains)
}

// EncodeDomains stores a validated list as JSON.
func EncodeDomains(domains []string) (string, error) {
	data, err := json.Marshal(domains)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseStoredDomains is the archive check: the stored JSON must already be the
// canonical form the panel writes, not merely something that normalizes into it.
func ParseStoredDomains(raw string) ([]string, error) {
	domains, err := ParseDomains(raw)
	if err != nil {
		return nil, err
	}
	canonical, err := EncodeDomains(domains)
	if err != nil || canonical != raw {
		return nil, ErrDomains
	}
	return domains, nil
}

// ValidEmail accepts an empty (unset) address and otherwise exactly one mailbox
// without a display name, which is what the CA takes as contact.
func ValidEmail(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 254 || strings.ContainsFunc(value, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && address.Name == ""
}

var (
	// ErrDomains reports an unusable name list.
	ErrDomains = errors.New("domains must be 1 to 10 lower-case DNS names, optionally with a leading *. wildcard")
	// ErrSettingsInvalid reports an unusable email, CA choice or revision.
	ErrSettingsInvalid = errors.New("certificate settings are invalid")
	// ErrSettingsConflict reports a stale expectedRevision.
	ErrSettingsConflict = errors.New("certificate settings changed; refresh before saving")
	// ErrSettingsMissing reports an issuance request without an email or token.
	ErrSettingsMissing = errors.New("certificate settings require an email address and a Cloudflare API token")
	// ErrTokenInvalid reports a token Cloudflare does not accept.
	ErrTokenInvalid = errors.New("Cloudflare API Token invalid")
	// ErrTokenUnverified reports that the token could not be checked at all, which
	// is different from a token Cloudflare rejects.
	ErrTokenUnverified = errors.New("Cloudflare API Token could not be verified")
	// ErrNodeUnknown reports an unknown node in the automatic DNS choice.
	ErrNodeUnknown = errors.New("node not found")
	// ErrNotFound reports an unknown certificate row.
	ErrNotFound = errors.New("certificate not found")
	// ErrInUse reports a certificate an inbound still references.
	ErrInUse = errors.New("certificate is referenced by an inbound")
)

// InUseError carries the inbounds that still reference a certificate, so the API
// can name them instead of only refusing the deletion.
type InUseError struct{ Inbounds []Usage }

func (e *InUseError) Error() string { return ErrInUse.Error() }

// Unwrap lets a caller match the refusal with errors.Is(err, ErrInUse).
func (e *InUseError) Unwrap() error { return ErrInUse }
