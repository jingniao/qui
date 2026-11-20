package importer

import (
	"context"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
)

// CachedSource isolates optional external cache failures from successfully
// published managed relays, whose failures must stop subscription generation.
func (s *Store) CachedSource() source.Source { return cachedSource{s} }
func (s *Store) RelaySource() source.Source  { return relaySource{s} }

type cachedSource struct{ store *Store }

func (s cachedSource) ID() string   { return s.store.ID() }
func (cachedSource) Optional() bool { return true }
func (s cachedSource) Proxies(ctx context.Context, viewer source.Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	return s.store.proxies(ctx, viewer, false)
}

type relaySource struct{ store *Store }

func (relaySource) ID() string     { return "published-import-relays" }
func (relaySource) Optional() bool { return false }
func (s relaySource) Proxies(ctx context.Context, viewer source.Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	items, err := s.store.importRelays(ctx, viewer)
	return items, nil, err
}
