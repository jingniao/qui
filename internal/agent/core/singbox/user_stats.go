package singbox

import (
	"context"
	"errors"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	stats "proxycontrol/pkg/contracts/xraystatspb"
)

const queryStatsMethod = "/v2ray.core.app.stats.command.StatsService/QueryStats"

type v2rayAPIOptions struct {
	Listen string            `json:"listen"`
	Stats  v2rayStatsOptions `json:"stats"`
}
type v2rayStatsOptions struct {
	Enabled  bool     `json:"enabled"`
	Users    []string `json:"users"`
	Inbounds []string `json:"inbounds"`
}

func configureUserStats(doc *document, state desiredstate.State, port int, result *core.RenderedConfig) error {
	if port < 1 || port > 65535 {
		return errors.New("invalid sing-box user statistics port")
	}
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if endpoint == result.StatsEndpoint {
		return errors.New("statistics listeners conflict")
	}
	options := &v2rayAPIOptions{Listen: endpoint, Stats: v2rayStatsOptions{Enabled: true, Users: []string{}, Inbounds: []string{}}}
	accounts := map[string]traffic.Counter{}
	names := map[string]desiredstate.Credential{}
	for _, entry := range state.Inbounds {
		if !entry.Enabled {
			continue
		}
		if entry.Port == port {
			return errors.New("inbound conflicts with private user statistics port")
		}
		if entry.Protocol == "wireguard" {
			continue
		}
		userInbound := false
		for _, id := range entry.CredentialIDs {
			var credential desiredstate.Credential
			for _, c := range state.Credentials {
				if c.ID == id {
					credential = c
					break
				}
			}
			name := credential.ID
			if credential.Protocol == "http" || credential.Protocol == "socks5" {
				name = credential.Username
			}
			if previous, ok := names[name]; ok && previous.ID != credential.ID {
				// HTTP and SOCKS share a username for the same access identity.
				if previous.Internal || credential.Internal || previous.UserID == "" || previous.UserID != credential.UserID || previous.DeviceID != credential.DeviceID || (previous.Protocol != "http" && previous.Protocol != "socks5") || (credential.Protocol != "http" && credential.Protocol != "socks5") {
					return errors.New("statistics account name is ambiguous")
				}
			}
			if credential.Internal || credential.UserID == "" || credential.DeviceID == "" {
				names[name] = credential
				continue
			}
			userInbound = true
			if _, exists := accounts["user>>>"+name]; !exists {
				accounts["user>>>"+name] = traffic.Counter{Revision: state.Revision, CredentialID: credential.ID, UserID: credential.UserID, DeviceID: credential.DeviceID, Protocol: credential.Protocol}
				options.Stats.Users = append(options.Stats.Users, name)
			}
			names[name] = credential
		}
		if userInbound {
			options.Stats.Inbounds = append(options.Stats.Inbounds, entry.ID)
		}
	}
	sort.Strings(options.Stats.Users)
	sort.Strings(options.Stats.Inbounds)
	if doc.Experimental == nil {
		doc.Experimental = &experimentalOptions{}
	}
	doc.Experimental.V2rayAPI = options
	doc.Route.Rules = append([]route{{Ports: []uint16{uint16(port)}, Action: "reject", origin: collector.Exit{Protocol: "block", LogDomain: false}}}, doc.Route.Rules...)
	result.UserStatsEndpoint = endpoint
	result.StatsAccounts = accounts
	result.HealthEndpoints = append(result.HealthEndpoints, endpoint)
	return nil
}

// readCounters reads cumulative native counters. The spool commits each changed
// sample and its epoch/credential cursor together; the panel computes deltas.
func readCounters(ctx context.Context, configuration core.RenderedConfig, epoch string, source *core.CounterSources) (sample []traffic.Counter, sampleErr error) {
	defer func() {
		if sampleErr != nil && source != nil {
			source.Failure(epoch)
		}
	}()
	host, _, err := net.SplitHostPort(configuration.UserStatsEndpoint)
	if err != nil || host != "127.0.0.1" || epoch == "" {
		return nil, errors.New("statistics require loopback and a process epoch")
	}
	connection, err := grpc.NewClient(configuration.UserStatsEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20)))
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	response := &stats.QueryStatsResponse{}
	// sing-box ignores Pattern. One empty query avoids overlapping responses and
	// includes lazy counters only once; filtering belongs entirely to the client.
	if err := connection.Invoke(queryCtx, queryStatsMethod, &stats.QueryStatsRequest{Reset_: false}, response); err != nil {
		return nil, errors.New("core statistics query failed")
	}
	if len(response.Stat) > 16384 {
		return nil, errors.New("core statistics response exceeds account limit")
	}
	collected := map[string]traffic.Counter{}
	seen := map[string]bool{}
	directions := map[string]uint8{}
	observed := time.Now().UTC()
	for _, stat := range response.Stat {
		if stat == nil {
			return nil, errors.New("invalid core statistic")
		}
		if !strings.HasPrefix(stat.Name, "user>>>") {
			continue
		}
		position := strings.LastIndex(stat.Name, ">>>traffic>>>")
		if position < 0 {
			continue
		}
		key := stat.Name[:position]
		template, known := configuration.StatsAccounts[key]
		if !known {
			continue
		}
		if stat.Value < 0 || seen[stat.Name] {
			return nil, errors.New("invalid or duplicate core statistic")
		}
		seen[stat.Name] = true
		counter, exists := collected[template.CredentialID]
		if !exists {
			counter = template
			counter.CoreEpoch = epoch
			counter.ObservedAt = observed
		}
		switch stat.Name[position+len(">>>traffic>>>"):] {
		case "uplink":
			directions[key] |= 1
			if math.MaxInt64-counter.UploadBytes < stat.Value {
				return nil, errors.New("core statistics overflow")
			}
			counter.UploadBytes += stat.Value
		case "downlink":
			directions[key] |= 2
			if math.MaxInt64-counter.DownloadBytes < stat.Value {
				return nil, errors.New("core statistics overflow")
			}
			counter.DownloadBytes += stat.Value
		default:
			return nil, errors.New("unknown core account traffic direction")
		}
		collected[template.CredentialID] = counter
	}
	for _, pair := range directions {
		if pair != 3 {
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
	if source != nil {
		if err := source.Observe(epoch, directions); err != nil {
			return nil, err
		}
	}
	return result, nil
}
