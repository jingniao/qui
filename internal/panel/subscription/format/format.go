// Package format holds the exporter registry a subscription is rendered
// through. An exporter turns the neutral proxy list into one client's format;
// the registry resolves a requested format ID or alias and, when the Owner
// allows it, a client User-Agent.
//
// Nothing here imports the panel's modules or an HTTP server, so a new format is
// one package plus one line of registration (acceptance A1.2, A1.5).
package format

import (
	"strconv"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

// Usage is what the subscription headers report about the account.
type Usage struct {
	Upload    int64
	Download  int64
	Total     int64
	ExpiresAt int64
}

// Header renders the `subscription-userinfo` value clients read their quota from.
// A zero Total means unlimited and a zero ExpiresAt means no expiry, which is the
// convention every client expects.
func (u Usage) Header() string {
	return "upload=" + strconv.FormatInt(u.Upload, 10) +
		"; download=" + strconv.FormatInt(u.Download, 10) +
		"; total=" + strconv.FormatInt(u.Total, 10) +
		"; expire=" + strconv.FormatInt(u.ExpiresAt, 10)
}

// Profile is everything an exporter renders.
type Profile struct {
	Proxies []proxy.Proxy
	Usage   Usage
	// Rules is the client rule template expanded for this user. It is set only
	// for exporters whose UsesRules reports true.
	Rules *rules.Resolved
	// ProviderURL returns the absolute URL a client fetches a panel service rule
	// from (rules.ResolvedProvider.ServiceSetID). It is nil when the profile is
	// rendered without a subscription token, for example in the Owner's preview.
	ProviderURL func(serviceSetID string) string
}

// Output is a rendered subscription plus the diagnostics of the rendering.
type Output struct {
	Body     []byte
	Warnings []proxy.Warning
}

// Policy is the Owner's subscription settings, as the panel stores them.
type Policy struct {
	// Default is the format used when no `?format=` is given and the client
	// User-Agent is unknown or detection is off.
	Default string
	// DetectUserAgent turns User-Agent based format detection on.
	DetectUserAgent bool
	// UpdateHours is the profile-update-interval a client is told to refresh at.
	UpdateHours int
}

// Exporter renders one client format.
type Exporter interface {
	// ID is the value of `?format=` and the value stored in settings.
	ID() string
	// Aliases are alternative IDs accepted for the same format.
	Aliases() []string
	// ContentType is the response content type.
	ContentType() string
	// FileName is the download name used in Content-Disposition.
	FileName() string
	// UserAgents lists the User-Agent patterns this format serves. No pattern is
	// shipped in v1.1: see docs/v1.1-phase0-findings.md A.7 for the strings that
	// still have to be verified from client sources. A pattern is matched as a
	// case-insensitive substring; a "!" prefix makes it a guard that must not
	// appear (for example "clashx/" guarded by "!meta").
	UserAgents() []string
	// UsesRules reports whether the format can carry the client rule template.
	UsesRules() bool
	// Render builds the document. A format leaves out what it cannot express and
	// reports it as a warning instead of failing the whole subscription.
	Render(Profile) (Output, error)
}

// Parser turns an external subscription into typed client proxies. The import
// scheduler calls parsers; subscription requests read only encrypted caches.
type Parser interface {
	Detect(content []byte) bool
	Parse(content []byte) ([]proxy.Proxy, error)
}

// Info is the serialisable description of an exporter.
type Info struct {
	ID          string   `json:"id"`
	Aliases     []string `json:"aliases"`
	ContentType string   `json:"contentType"`
	FileName    string   `json:"fileName"`
	UsesRules   bool     `json:"usesRules"`
}
