package singbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"strconv"
)

// experimentalOptions renders only upstream sing-box experimental modules.
// Official builds have no v2ray API (plan D5) and no fork connection module, so
// the Clash API is the only control surface the agent may render.
type experimentalOptions struct {
	ClashAPI *clashAPIOptions `json:"clash_api,omitempty"`
}
type clashAPIOptions struct {
	ExternalController string `json:"external_controller"`
	Secret             string `json:"secret"`
}

// NewStatsSecret returns the random secret guarding one configuration's
// loopback-only Clash API. The Agent generates it per rendered configuration and
// keeps it for the statistics reader of step 6.3.
func NewStatsSecret() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func configureStats(doc *document, state desiredstate.State, port int, secret string, result *core.RenderedConfig) error {
	if port < 1 || port > 65535 {
		return errors.New("invalid sing-box statistics port")
	}
	if len(secret) < 32 {
		return errors.New("sing-box statistics secret missing")
	}
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for _, entry := range state.Inbounds {
		if entry.Enabled && entry.Port == port {
			return errors.New("inbound conflicts with private statistics port")
		}
	}
	doc.Experimental = &experimentalOptions{ClashAPI: &clashAPIOptions{ExternalController: endpoint, Secret: secret}}
	// Block the reserved port before all business rules, for IPs and hostnames
	// alike. The Agent's direct loopback connection never traverses this route.
	doc.Route.Rules = append([]route{{Ports: []uint16{uint16(port)}, Action: "reject", origin: collector.Exit{Protocol: "block", LogDomain: false}}}, doc.Route.Rules...)
	result.StatsEndpoint = endpoint
	result.HealthEndpoints = append(result.HealthEndpoints, endpoint)
	return nil
}

// TrafficCounters reports per-user byte deltas for the running configuration.
// Official sing-box builds expose no per-user statistics: the release binaries
// are not built with the v2ray API (plan D5) and the Clash API does not name the
// user of a connection. The adapter therefore reports the source as unavailable
// instead of inventing a value; the node totals it can read live in
// NodeTrafficTotals.
func (a *Adapter) TrafficCounters(context.Context) ([]traffic.Counter, error) {
	return nil, ErrTelemetryUnavailable
}

func (a *Adapter) SampleTraffic(context.Context) error {
	return ErrTelemetryUnavailable
}
