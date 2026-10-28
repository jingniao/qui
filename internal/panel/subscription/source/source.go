// Package source collects the client proxies a subscription is built from. A
// source knows one place proxies come from: today the panel's own published
// nodes, later an imported external subscription.
//
// Sources never render anything and never see the requested format, so adding a
// source changes no exporter (acceptance A1.2).
package source

import (
	"context"

	"proxycontrol/internal/panel/subscription/proxy"
)

// Viewer is the account a subscription is rendered for. It carries only what a
// source needs to filter its own proxies.
type Viewer struct {
	UserID           string
	DeviceID         string
	AllowedProtocols []string
	NodeGroupIDs     []string
}

// AllowsProtocol reports whether the account may use the protocol.
func (v Viewer) AllowsProtocol(protocol string) bool {
	for _, allowed := range v.AllowedProtocols {
		if allowed == protocol {
			return true
		}
	}
	return false
}

// GrantsGroup reports whether the account was granted the node group.
func (v Viewer) GrantsGroup(groupID string) bool {
	for _, granted := range v.NodeGroupIDs {
		if granted == groupID {
			return true
		}
	}
	return false
}

// Source produces client proxies for one account.
type Source interface {
	// ID names the source in warnings and logs.
	ID() string
	// Proxies returns its proxies. A source that cannot answer returns an error
	// and contributes nothing.
	Proxies(ctx context.Context, viewer Viewer) ([]proxy.Proxy, []proxy.Warning, error)
}

// Optional marks a source whose failure is not fatal to the subscription: the
// registry drops it, keeps a warning and renders the other sources.
//
// The primary source (the panel's own published nodes) is deliberately not
// optional. Serving an empty but successful subscription when the database read
// fails would make every client wipe its server list, so its errors stay fatal.
type Optional interface {
	Optional() bool
}

// Registry is the ordered set of sources of one subscription.
type Registry struct {
	sources []Source
}

// NewRegistry builds a registry in precedence order: the first source wins a
// name collision.
func NewRegistry(sources ...Source) *Registry {
	r := &Registry{}
	for _, item := range sources {
		r.Register(item)
	}
	return r
}

// Register appends a source. A duplicate ID is ignored, so a source is never
// collected twice.
func (r *Registry) Register(item Source) {
	if item == nil || item.ID() == "" {
		return
	}
	for _, existing := range r.sources {
		if existing.ID() == item.ID() {
			return
		}
	}
	r.sources = append(r.sources, item)
}

// Sources lists the registered sources in precedence order.
func (r *Registry) Sources() []Source { return append([]Source(nil), r.sources...) }

// Collect asks every source for its proxies and merges the answer.
//
// A failing optional source contributes nothing and a warning; any other error
// stops the collection. A name collision is resolved in favour of the earlier
// source, and the dropped proxy is reported instead of silently replacing a
// working one.
func (r *Registry) Collect(ctx context.Context, viewer Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	proxies := []proxy.Proxy{}
	warnings := []proxy.Warning{}
	seen := map[string]string{}
	for _, item := range r.sources {
		produced, raised, err := item.Proxies(ctx, viewer)
		if err != nil {
			optional, ok := item.(Optional)
			if !ok || !optional.Optional() {
				return nil, warnings, err
			}
			warnings = append(warnings, proxy.SourceWarning(item.ID(), "source unavailable: "+err.Error()))
			continue
		}
		warnings = append(warnings, raised...)
		for _, endpoint := range produced {
			if owner, taken := seen[endpoint.Name]; taken {
				warnings = append(warnings, proxy.SourceWarning(item.ID(), "proxy \""+endpoint.Name+"\" of source "+owner+" was kept; the duplicate was dropped"))
				continue
			}
			seen[endpoint.Name] = item.ID()
			proxies = append(proxies, endpoint)
		}
	}
	return proxies, warnings, nil
}
