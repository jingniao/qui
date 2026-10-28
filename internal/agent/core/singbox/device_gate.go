package singbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"proxycontrol/pkg/contracts/traffic"
	"sort"
	"time"
)

type sourceConnection struct {
	ID       string `json:"id"`
	Metadata struct {
		SourceIP   string `json:"sourceIP"`
		SourcePort string `json:"sourcePort"`
	} `json:"metadata"`
}

func sourceEndpoint(c sourceConnection) (string, bool) {
	ap, err := netip.ParseAddrPort(net.JoinHostPort(c.Metadata.SourceIP, c.Metadata.SourcePort))
	if err != nil || ap.Port() == 0 || ap.Addr().Zone() != "" {
		return "", false
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()).String(), true
}
func (a *Adapter) clash(ctx context.Context, method, path string) ([]byte, error) {
	host, _, err := net.SplitHostPort(a.current.StatsEndpoint)
	if err != nil || host != "127.0.0.1" || a.statsSecret == "" {
		return nil, errors.New("device observation requires authenticated loopback")
	}
	r, err := http.NewRequestWithContext(ctx, method, "http://"+a.current.StatsEndpoint+path, nil)
	if err != nil {
		return nil, errors.New("device observation request failed")
	}
	r.Header.Set("Authorization", "Bearer "+a.statsSecret)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 2 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		return nil, errors.New("device observation API unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, errors.New("device observation API rejected request")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("device observation API response invalid")
	}
	return data, nil
}
func (a *Adapter) sourceConnections(ctx context.Context) ([]sourceConnection, map[string]string, error) {
	if a.process == nil || a.sourceWriter == nil {
		return nil, nil, errors.New("device observation unavailable")
	}
	data, err := a.clash(ctx, http.MethodGet, "/connections")
	if err != nil {
		return nil, nil, err
	}
	var doc struct {
		Connections *[]sourceConnection `json:"connections"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.Connections == nil {
		return nil, nil, errors.New("device observation API fields unavailable")
	}
	active := map[string]bool{}
	for _, c := range *doc.Connections {
		ep, ok := sourceEndpoint(c)
		if !ok || c.ID == "" {
			return nil, nil, errors.New("device observation API fields unavailable")
		}
		active[ep] = true
	}
	users, ok := a.sourceWriter.SourceUsers(active)
	if !ok {
		return nil, nil, errors.New("device observation source unavailable")
	}
	return *doc.Connections, users, nil
}
func (a *Adapter) ConnectionSnapshot(ctx context.Context) (traffic.ConnectionSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	list, users, err := a.sourceConnections(ctx)
	if err != nil {
		a.gateFailed = true
		a.deviceGate.Clear()
		return traffic.ConnectionSnapshot{}, err
	}
	keys := map[string]map[string]bool{}
	fullIPs := map[string]map[string]bool{}
	for _, in := range a.current.AccessContext.Inbounds {
		if !in.Internal && in.Protocol != "wireguard" {
			for _, account := range in.Accounts {
				if !account.Internal {
					keys[account.ID] = map[string]bool{}
					fullIPs[account.ID] = map[string]bool{}
				}
			}
		}
	}
	complete := true
	for _, c := range list {
		ep, _ := sourceEndpoint(c)
		cid, joined := users[ep]
		if !joined {
			complete = false
		}
		if keys[cid] == nil {
			continue
		}
		if key, ok := traffic.AddressKey(c.Metadata.SourceIP); ok {
			keys[cid][key] = true
			fullIP, _ := netip.ParseAddr(c.Metadata.SourceIP)
			fullIPs[cid][fullIP.Unmap().String()] = true
		}
	}
	if !complete {
		a.deviceGate.Clear()
		a.gateFailed = true
		return traffic.ConnectionSnapshot{}, errors.New("device observation join incomplete")
	}
	a.gateFailed = false
	s := traffic.ConnectionSnapshot{NodeID: a.current.NodeID, Revision: a.current.Revision, CoreEpoch: a.statsEpoch, ObservedAt: a.gateNow().UTC(), Scope: traffic.ConnectionScope, ObservationContractVersion: 2, DeviceGateSupported: complete && !a.gateFailed}
	ids := []string{}
	for id := range keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		account := traffic.ConnectionAccount{CredentialID: id, AddressesMeasured: complete}
		for key := range keys[id] {
			account.AddressKeys = append(account.AddressKeys, key)
		}
		sort.Strings(account.AddressKeys)
		account.Connections = uint64(len(fullIPs[id]))
		s.Connections += account.Connections
		s.Accounts = append(s.Accounts, account)
	}
	return s, s.Validate()
}
func (a *Adapter) gateNow() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
func (a *Adapter) SetDeviceGate(ctx context.Context, data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.deviceGate.Set(data, a.gateNow()); err != nil {
		return err
	}
	return a.tickDeviceGateLocked(ctx)
}
func (a *Adapter) TickDeviceGates(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tickDeviceGateLocked(ctx)
}
func (a *Adapter) tickDeviceGateLocked(ctx context.Context) error {
	entries := a.deviceGate.Entries(a.gateNow())
	if len(entries) == 0 {
		return nil
	}
	list, users, err := a.sourceConnections(ctx)
	if err != nil {
		a.deviceGate.Clear()
		a.gateFailed = true
		return err
	}
	gated := map[string]map[string]bool{}
	for _, e := range entries {
		if gated[e.CredentialID] == nil {
			gated[e.CredentialID] = map[string]bool{}
		}
		for _, k := range e.Keys {
			gated[e.CredentialID][k] = true
		}
	}
	for _, c := range list {
		ep, _ := sourceEndpoint(c)
		key, ok := traffic.AddressKey(c.Metadata.SourceIP)
		if ok && gated[users[ep]][key] {
			if _, err := a.clash(ctx, http.MethodDelete, "/connections/"+url.PathEscape(c.ID)); err != nil {
				a.deviceGate.Clear()
				a.gateFailed = true
				return err
			}
		}
	}
	a.gateFailed = false
	return nil
}
