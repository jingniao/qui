package format

import (
	"errors"
	"strings"

	"proxycontrol/internal/panel/subscription/proxy"
)

// ErrUnknown reports a format ID that no exporter registered.
var ErrUnknown = errors.New("unknown subscription format")

// Registry resolves format IDs and client User-Agents to exporters.
//
// Exporters are kept in registration order, which is the order of the
// `GET /api/v1/subscription-formats` list and the tie-break of an equally
// specific User-Agent match.
type Registry struct {
	exporters []Exporter
	// Parsers are explicitly registered by the subscription service.
	parsers []Parser
}

// NewRegistry builds a registry from the given exporters.
func NewRegistry(exporters ...Exporter) *Registry {
	r := &Registry{}
	for _, exporter := range exporters {
		r.Register(exporter)
	}
	return r
}

// Register adds an exporter. A later exporter may reuse neither an ID nor an
// alias of an earlier one; the duplicate is ignored so that a registry can never
// resolve ambiguously.
func (r *Registry) Register(exporter Exporter) {
	if exporter == nil || exporter.ID() == "" || r.Known(exporter.ID()) {
		return
	}
	// A conflicting alias would make the registry ambiguous, so the exporter is
	// refused; an empty alias is simply skipped.
	for _, alias := range exporter.Aliases() {
		if alias != "" && r.knownValue(alias) {
			return
		}
	}
	r.exporters = append(r.exporters, exporter)
}

func (r *Registry) knownValue(value string) bool {
	for _, exporter := range r.exporters {
		if exporter.ID() == value {
			return true
		}
		for _, alias := range exporter.Aliases() {
			if alias == value {
				return true
			}
		}
	}
	return false
}

// Get resolves an ID or an alias.
func (r *Registry) Get(id string) (Exporter, bool) {
	for _, exporter := range r.exporters {
		if exporter.ID() == id {
			return exporter, true
		}
		for _, alias := range exporter.Aliases() {
			if alias == id {
				return exporter, true
			}
		}
	}
	return nil, false
}

// Known reports whether an ID or alias is registered. The panel validates the
// stored default format and the runtime policy with it.
func (r *Registry) Known(id string) bool {
	_, known := r.Get(id)
	return known
}

// Exporters lists the registered exporters in registration order.
func (r *Registry) Exporters() []Exporter {
	return append([]Exporter(nil), r.exporters...)
}

// Info describes the registered exporters for the admin API.
func (r *Registry) Info() []Info {
	items := make([]Info, 0, len(r.exporters))
	for _, exporter := range r.exporters {
		items = append(items, Info{ID: exporter.ID(), Aliases: append([]string{}, exporter.Aliases()...), ContentType: exporter.ContentType(), FileName: exporter.FileName(), UsesRules: exporter.UsesRules()})
	}
	return items
}

// Parsers lists the registered typed import parsers.
func (r *Registry) Parsers() []Parser { return append([]Parser(nil), r.parsers...) }

// RegisterParser registers a typed client import parser.
func (r *Registry) RegisterParser(p Parser) {
	if p != nil {
		r.parsers = append(r.parsers, p)
	}
}

// Resolve picks the exporter for one request:
//
//  1. an explicit `?format=` wins, and an unknown one is an error;
//  2. otherwise the client User-Agent, when the Owner turned detection on;
//  3. otherwise the Owner default; an unregistered default falls back to the
//     first registered exporter with a warning.
func (r *Registry) Resolve(requested, userAgent string, policy Policy) (Exporter, []proxy.Warning, error) {
	if requested != "" {
		exporter, ok := r.Get(requested)
		if !ok {
			return nil, nil, ErrUnknown
		}
		return exporter, nil, nil
	}
	if policy.DetectUserAgent {
		if exporter, ok := r.Match(userAgent); ok {
			return exporter, nil, nil
		}
	}
	if exporter, ok := r.Get(policy.Default); ok {
		return exporter, nil, nil
	}
	if len(r.exporters) == 0 {
		return nil, nil, ErrUnknown
	}
	fallback := r.exporters[0]
	return fallback, []proxy.Warning{proxy.FormatWarning(fallback.ID(), "the configured default format \""+policy.Default+"\" is not registered; served "+fallback.ID()+" instead")}, nil
}

// Match resolves a User-Agent to an exporter. The most specific pattern wins: a
// longer matched pattern beats a shorter one, and a tie keeps the exporter that
// was registered first, so `clashforwindows` beats a bare `clash`.
//
// No pattern ships in v1.1 (docs/v1.1-phase0-findings.md A.7); the resolver is
// complete so that verified strings are added as data, not as code.
func (r *Registry) Match(userAgent string) (Exporter, bool) {
	if userAgent == "" {
		return nil, false
	}
	lowered := strings.ToLower(userAgent)
	var matched Exporter
	specificity := 0
	for _, exporter := range r.exporters {
		length, ok := matchUserAgent(exporter.UserAgents(), lowered)
		if ok && length > specificity {
			matched, specificity = exporter, length
		}
	}
	return matched, matched != nil
}

// matchUserAgent reports the length of the longest positive pattern that matched
// and whether the User-Agent is a candidate at all.
func matchUserAgent(patterns []string, lowered string) (int, bool) {
	specificity := 0
	for _, pattern := range patterns {
		if guard, ok := strings.CutPrefix(pattern, "!"); ok {
			if guard != "" && strings.Contains(lowered, strings.ToLower(guard)) {
				return 0, false
			}
			continue
		}
		if pattern == "" {
			continue
		}
		if strings.Contains(lowered, strings.ToLower(pattern)) && len(pattern) > specificity {
			specificity = len(pattern)
		}
	}
	return specificity, specificity > 0
}
