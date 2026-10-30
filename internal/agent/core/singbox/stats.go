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

// experimentalOptions renders upstream modules only. The v2ray API is enabled
// separately when the installed binary advertises its build tag.
type experimentalOptions struct {
	ClashAPI *clashAPIOptions `json:"clash_api,omitempty"`
	V2rayAPI *v2rayAPIOptions `json:"v2ray_api,omitempty"`
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

func configureStats(doc *document, state desiredstate.State, port int, secret string, result *core.RenderedConfig, reserved ...int) error {
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
	ports := []uint16{uint16(port)}
	if len(reserved) > 0 {
		ports = nil
		for _, p := range reserved {
			ports = append(ports, uint16(p))
		}
	}
	doc.Route.Rules = append([]route{{Ports: ports, Action: "reject", origin: collector.Exit{Protocol: "block", LogDomain: false}}}, doc.Route.Rules...)
	result.StatsEndpoint = endpoint
	result.HealthEndpoints = append(result.HealthEndpoints, endpoint)
	return nil
}

// TrafficCounters returns cumulative per-credential counters for one process
// epoch. Builds without the v2ray API retain the unavailable D5 behavior.
func (a *Adapter) TrafficCounters(ctx context.Context) ([]traffic.Counter, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current.UserStatsEndpoint == "" {
		return nil, ErrTelemetryUnavailable
	}
	if a.process == nil {
		return nil, errors.New("core not running")
	}
	return readCounters(ctx, a.current, a.statsEpoch, &a.counterSources)
}

func (a *Adapter) SampleTraffic(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sampleTrafficLocked(ctx)
}

func (a *Adapter) sampleTrafficLocked(ctx context.Context) error {
	if a.process == nil || a.hooks.TrafficSink == nil {
		return nil
	}
	if a.current.UserStatsEndpoint == "" {
		if a.current.StatsEndpoint == "" || a.statsSecret == "" {
			return ErrTelemetryUnavailable
		}
		totals, err := readClashTotals(ctx, a.current.StatsEndpoint, a.statsSecret)
		if err != nil {
			return err
		}
		node, err := core.NodeCounter("sing-box", a.statsEpoch, a.current.Revision, totals.UploadTotal, totals.DownloadTotal)
		if err != nil {
			return err
		}
		return a.hooks.TrafficSink(ctx, []traffic.Counter{node})
	}
	counters, err := readCounters(ctx, a.current, a.statsEpoch, &a.counterSources)
	if err != nil {
		return err
	}
	return core.SaveAccountAndNode(ctx, a.hooks.TrafficSink, "sing-box", a.statsEpoch, a.current.Revision, counters)
}
