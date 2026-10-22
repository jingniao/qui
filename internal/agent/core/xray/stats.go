package xray

import (
	"context"
	"errors"
	"math"
	"net"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	stats "proxycontrol/pkg/contracts/xraystatspb"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type statsAPIConfig struct {
	Tag      string   `json:"tag"`
	Listen   string   `json:"listen"`
	Services []string `json:"services"`
}
type statsPolicyConfig struct {
	Levels map[string]statsUserPolicy `json:"levels"`
	System *statsSystemPolicy         `json:"system,omitempty"`
}
type statsSystemPolicy struct {
	InboundUpload   bool `json:"statsInboundUplink"`
	InboundDownload bool `json:"statsInboundDownlink"`
	Upload          bool `json:"statsOutboundUplink"`
	Download        bool `json:"statsOutboundDownlink"`
}
type statsUserPolicy struct {
	Upload   bool `json:"statsUserUplink"`
	Download bool `json:"statsUserDownlink"`
	Online   bool `json:"statsUserOnline,omitempty"`
}

func configureStats(document *configDocument, state desiredstate.State, port int, options RenderOptions, rendered *core.RenderedConfig) error {
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	document.API = &statsAPIConfig{Tag: "_stats_api_", Listen: endpoint, Services: []string{"StatsService"}}
	document.Stats = &struct{}{}
	document.Policy = &statsPolicyConfig{Levels: map[string]statsUserPolicy{"0": {Upload: true, Download: true, Online: options.OnlineStats}}}
	// policy.system enables the per-inbound counters plus, when a WireGuard
	// device credential needs per-device attribution, the per-outbound ones the
	// reader queries.
	system := &statsSystemPolicy{InboundUpload: true, InboundDownload: true}
	for _, credential := range state.Credentials {
		if !credential.Internal && credential.Protocol == "wireguard" {
			system.Upload, system.Download = true, true
			break
		}
	}
	document.Policy.System = system
	inbounds := []string{}
	for _, inbound := range document.Inbounds {
		inbounds = append(inbounds, inbound.Tag)
	}
	if len(inbounds) > 0 {
		guard := "_stats_guard_block_"
		document.Outbounds = append(document.Outbounds, outboundConfig{Tag: guard, Protocol: "blackhole", Settings: map[string]any{}})
		if document.accessOrigins != nil {
			document.accessOrigins[guard] = collector.Exit{Protocol: "block", LogDomain: true}
		}
		document.Routing.Rules = append([]route{{Type: "field", InboundTag: inbounds, Port: strconv.Itoa(port), Network: "tcp,udp", OutboundTag: guard}}, document.Routing.Rules...)
	}
	rendered.StatsEndpoint = endpoint
	rendered.OnlineStats = options.OnlineStats
	rendered.HealthEndpoints = append(rendered.HealthEndpoints, endpoint)
	rendered.StatsAccounts = map[string]traffic.Counter{}
	// User counters are global to the core, including internal relay users.
	// Reject aliases before excluding internal accounts from user accounting.
	names := map[string]desiredstate.Credential{}
	for _, credential := range state.Credentials {
		bound := false
		for _, inbound := range state.Inbounds {
			if inbound.Enabled && inList(inbound.CredentialIDs, credential.ID) {
				bound = true
			}
		}
		if !bound {
			continue
		}
		name := credentialEmail(credential)
		if credential.Protocol == "http" || credential.Protocol == "socks5" {
			name = credential.Username
		}
		if previous, exists := names[name]; exists && previous.ID != credential.ID {
			// The panel gives a device's HTTP and SOCKS5 credentials one
			// username, so Xray keeps one counter for both. It still belongs to
			// a single user and device and is counted once.
			if !sameDeviceUsername(previous, credential) {
				return errors.New("statistics account name is ambiguous")
			}
			continue
		}
		names[name] = credential
		if credential.Internal || credential.UserID == "" || credential.DeviceID == "" {
			continue
		}
		if credential.Protocol == "wireguard" {
			for tag, exit := range document.accessOrigins {
				if exit.AccountID == credential.ID {
					rendered.StatsAccounts["outbound>>>"+tag] = traffic.Counter{Revision: state.Revision, CredentialID: credential.ID, UserID: credential.UserID, DeviceID: credential.DeviceID, Protocol: credential.Protocol}
				}
			}
			continue
		}
		rendered.StatsAccounts["user>>>"+name] = traffic.Counter{Revision: state.Revision, CredentialID: credential.ID, UserID: credential.UserID, DeviceID: credential.DeviceID, Protocol: credential.Protocol}
	}
	return nil
}

func readCounters(ctx context.Context, configuration core.RenderedConfig, epoch string, sources ...*core.CounterSources) (sample []traffic.Counter, sampleErr error) {
	defer func() {
		if sampleErr != nil {
			for _, source := range sources {
				if source != nil {
					source.Failure(epoch)
				}
			}
		}
	}()
	if configuration.StatsEndpoint == "" {
		return nil, errors.New("core statistics endpoint unavailable")
	}
	if host, _, err := net.SplitHostPort(configuration.StatsEndpoint); err != nil || host != "127.0.0.1" || epoch == "" {
		return nil, errors.New("statistics require loopback and a process epoch")
	}
	connection, err := grpc.NewClient(configuration.StatsEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20)))
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	patterns := []string{"user>>>"}
	for key := range configuration.StatsAccounts {
		if strings.HasPrefix(key, "outbound>>>") {
			patterns = append(patterns, "outbound>>>_access")
			break
		}
	}
	values := []*stats.Stat{}
	for _, pattern := range patterns {
		response, err := stats.NewStatsServiceClient(connection).QueryStats(queryCtx, &stats.QueryStatsRequest{Pattern: pattern, Reset_: false})
		if err != nil {
			return nil, errors.New("core statistics query failed")
		}
		if len(response.Stat) > 16384 {
			return nil, errors.New("core statistics response exceeds account limit")
		}
		values = append(values, response.Stat...)
	}
	collected := map[string]traffic.Counter{}
	seen := map[string]bool{}
	directions := map[string]uint8{}
	observed := time.Now().UTC()
	for _, stat := range values {
		if stat == nil || stat.Value < 0 || seen[stat.Name] {
			return nil, errors.New("invalid or duplicate core statistic")
		}
		seen[stat.Name] = true
		position := strings.LastIndex(stat.Name, ">>>traffic>>>")
		if position < 0 {
			continue
		}
		template, known := configuration.StatsAccounts[stat.Name[:position]]
		if !known {
			continue
		}
		counter, exists := collected[template.CredentialID]
		if !exists {
			counter = template
			counter.CoreEpoch = epoch
			counter.ObservedAt = observed
		}
		switch stat.Name[position+len(">>>traffic>>>"):] {
		case "uplink":
			directions[stat.Name[:position]] |= 1
			if math.MaxInt64-counter.UploadBytes < stat.Value {
				return nil, errors.New("core statistics overflow")
			}
			counter.UploadBytes += stat.Value
		case "downlink":
			directions[stat.Name[:position]] |= 2
			if math.MaxInt64-counter.DownloadBytes < stat.Value {
				return nil, errors.New("core statistics overflow")
			}
			counter.DownloadBytes += stat.Value
		default:
			return nil, errors.New("unknown core account traffic direction")
		}
		collected[template.CredentialID] = counter
	}
	// A WireGuard device can have several dedicated route tags. Require a
	// complete pair for each observed tag before summing those distinct sources.
	for _, direction := range directions {
		if direction != 3 {
			return nil, errors.New("incomplete core account counter pair")
		}
	}
	result := []traffic.Counter{}
	for _, counter := range collected {
		result = append(result, counter)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CredentialID < result[j].CredentialID })
	if len(result) > 0 {
		if err := traffic.ValidateSample(configuration.NodeID, result); err != nil {
			return nil, err
		}
	}
	for _, source := range sources {
		if source != nil {
			if err := source.Observe(epoch, directions); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (a *Adapter) SampleTraffic(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sampleTrafficLocked(ctx)
}
func (a *Adapter) sampleTrafficLocked(ctx context.Context) error {
	if a.process == nil {
		return nil
	}
	if a.TrafficSink == nil || a.current.StatsEndpoint == "" {
		return nil
	}
	counters, err := readCounters(ctx, a.current, a.statsEpoch, &a.counterSources)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err := a.TrafficSink(ctx, counters); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (a *Adapter) TrafficCounters(ctx context.Context) ([]traffic.Counter, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process == nil {
		return nil, errors.New("core not running")
	}
	return readCounters(ctx, a.current, a.statsEpoch, &a.counterSources)
}

func sameDeviceUsername(a, b desiredstate.Credential) bool {
	usernameKeyed := func(c desiredstate.Credential) bool { return c.Protocol == "http" || c.Protocol == "socks5" }
	return usernameKeyed(a) && usernameKeyed(b) && !a.Internal && !b.Internal && a.UserID != "" && a.UserID == b.UserID && a.DeviceID != "" && a.DeviceID == b.DeviceID
}
