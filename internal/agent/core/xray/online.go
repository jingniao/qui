package xray

import (
	"context"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net"
	"proxycontrol/pkg/contracts/traffic"
	stats "proxycontrol/pkg/contracts/xraystatspb"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ConnectionSnapshot reads the upstream per-user online statistics of the running
// configuration. Xray counts the distinct client IPs currently tracked for a user
// (policy.levels.0.statsUserOnline, Xray >= 24.11.11); it is not a TCP session
// count, and a user with several connections from one address counts once. When
// the running core does not support or enable online statistics the count is
// unknown, and the reader reports an error instead of a fabricated zero.
//
// WireGuard device credentials are absent from the snapshot: upstream registers
// no online map for a peer, so their count stays unknown (plan D4).
func (a *Adapter) ConnectionSnapshot(ctx context.Context) (traffic.ConnectionSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var sample traffic.ConnectionSnapshot
	if a.process == nil || !a.current.OnlineStats || a.current.StatsEndpoint == "" || a.statsEpoch == "" {
		return sample, errors.New("core online statistics unavailable")
	}
	host, _, err := net.SplitHostPort(a.current.StatsEndpoint)
	if err != nil || host != "127.0.0.1" {
		return sample, errors.New("online statistics require loopback")
	}
	connection, err := grpc.NewClient(a.current.StatsEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20)))
	if err != nil {
		return sample, err
	}
	defer connection.Close()
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	names := make([]string, 0, len(a.current.StatsAccounts))
	for name := range a.current.StatsAccounts {
		if strings.HasPrefix(name, "user>>>") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	accounts := make([]traffic.ConnectionAccount, 0, len(names))
	var total uint64
	for _, name := range names {
		value, err := stats.GetStatsOnline(queryCtx, connection, name+">>>online")
		if err != nil {
			return traffic.ConnectionSnapshot{}, errors.New("online statistics query failed")
		}
		if value < 0 || value > 100000000 {
			return traffic.ConnectionSnapshot{}, errors.New("online count exceeds bounds")
		}
		account := traffic.ConnectionAccount{CredentialID: a.current.StatsAccounts[name].CredentialID, Connections: uint64(value)}
		ips, ipErr := stats.NewStatsServiceClient(connection).GetStatsOnlineIpList(queryCtx, &stats.GetStatsOnlineIpListRequest{Name: name + ">>>online"})
		if ipErr == nil || status.Code(ipErr) == codes.NotFound {
			account.AddressesMeasured = true
			keys := map[string]bool{}
			if ips != nil {
				for raw := range ips.Ips {
					if key, ok := traffic.AddressKey(raw); ok {
						keys[key] = true
					}
				}
			}
			for key := range keys {
				account.AddressKeys = append(account.AddressKeys, key)
			}
			sort.Strings(account.AddressKeys)
		}
		accounts = append(accounts, account)
		total += uint64(value)
	}
	sample = traffic.ConnectionSnapshot{ObservationContractVersion: 2, DeviceGateSupported: a.probeDeviceGate(ctx), NodeID: a.current.NodeID, Revision: a.current.Revision, CoreEpoch: a.statsEpoch, ObservedAt: a.gateNow().UTC(), Scope: traffic.ConnectionScope, Connections: total, Accounts: accounts}
	for _, account := range sample.Accounts {
		if !account.AddressesMeasured {
			sample.DeviceGateSupported = false
		}
	}
	return sample, sample.Validate()
}
