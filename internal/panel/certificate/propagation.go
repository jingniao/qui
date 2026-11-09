package certificate

import (
	"context"
	"net"
	"time"
)

// Propagation waits until the challenge value is visible on the zone's
// authoritative name servers, which are the servers the CA itself asks.
type Propagation interface {
	Wait(ctx context.Context, zone Zone, name, value string) error
}

// DNSPropagator is the production checker. Lookup is what a test replaces; the
// production value asks one name server directly over UDP port 53.
type DNSPropagator struct {
	Poll    time.Duration
	Timeout time.Duration
	Lookup  func(ctx context.Context, server, name string) ([]string, error)
}

// Wait polls every name server of the zone until all of them answer with the
// value, then gives up after the timeout with the fixed propagation code.
func (p *DNSPropagator) Wait(ctx context.Context, zone Zone, name, value string) error {
	poll, timeout := p.Poll, p.Timeout
	if poll <= 0 {
		poll = 5 * time.Second
	}
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	lookup := p.Lookup
	if lookup == nil {
		lookup = lookupTXT
	}
	// A zone response without name_servers leaves the system resolver as the only
	// thing to ask, which still answers "not yet" until the record exists.
	servers := zone.NameServers
	if len(servers) == 0 {
		servers = []string{""}
	}
	deadline := time.Now().Add(timeout)
	for {
		ready := true
		for _, server := range servers {
			values, err := lookup(ctx, server, name)
			if err != nil || !containsValue(values, value) {
				ready = false
			}
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fail(CodeDNSPropagationTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

func containsValue(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// lookupTXT reads the TXT records of one name. An empty server means the system
// resolver; otherwise the query goes straight to that name server.
func lookupTXT(ctx context.Context, server, name string) ([]string, error) {
	resolver := net.DefaultResolver
	if server != "" {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "udp", net.JoinHostPort(server, "53"))
		}}
	}
	return resolver.LookupTXT(ctx, name)
}
