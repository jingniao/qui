package user

import (
	"context"
	"encoding/json"
	"proxycontrol/internal/panel/connections"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/devicegate"
	"sort"
	"sync"
	"time"
)

// DeviceObserver resolves only applied entry credentials before passing source
// keys to Admission. No address-bearing object is given to a repository/logger.
type DeviceObserver struct {
	Repository *Repository
	Nodes      *node.Repository
	States     interface {
		AppliedState(context.Context, string, int64) (desiredstate.State, error)
		Published(context.Context, string) (desiredstate.State, error)
	}
	Store       *connections.Store
	Admission   *connections.Admission
	Publish     func(string, any)
	Now         func() time.Time
	mu          sync.Mutex
	alerted     map[string]time.Time
	subscribers map[chan struct{}]bool
}
type DevicesOnline struct {
	Items            []connections.DeviceView `json:"items"`
	OnlineDevices    *int                     `json:"onlineDevices"`
	DeviceLimit      int                      `json:"deviceLimit"`
	Enforced         bool                     `json:"deviceLimitEnforced"`
	UnsupportedNodes []string                 `json:"deviceLimitUnsupportedNodes"`
}

func entryCredentials(state desiredstate.State) map[string]string {
	bound := map[string]bool{}
	for _, in := range state.Inbounds {
		if in.Enabled && in.Protocol != "wireguard" {
			for _, id := range in.CredentialIDs {
				bound[id] = true
			}
		}
	}
	result := map[string]string{}
	for _, c := range state.Credentials {
		if bound[c.ID] && !c.Internal && c.Protocol != "wireguard" && c.UserID != "" {
			result[c.ID] = c.UserID
		}
	}
	return result
}
func (o *DeviceObserver) Record(ctx context.Context, nodeID string) {
	if o == nil {
		return
	}
	snapshot, ok := o.Store.Snapshot(nodeID)
	if !ok || snapshot.ObservationContractVersion != 2 {
		return
	}
	state, err := o.States.AppliedState(ctx, nodeID, snapshot.Revision)
	if err != nil {
		return
	}
	allowed := entryCredentials(state)
	owners, err := o.Repository.CredentialOwners(ctx, keysOf(allowed))
	if err != nil {
		return
	}
	defer o.Changed()
	for _, account := range snapshot.Accounts {
		uid := allowed[account.CredentialID]
		if uid != "" && owners[account.CredentialID] == uid && account.AddressesMeasured {
			o.Admission.Observe(uid, nodeID, account.AddressKeys, false)
		}
	}
}
func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Reset discards the old admission order, then immediately rebuilds it from
// fresh applied snapshots. It does not turn a known live user into unknown or
// wait for another heartbeat to restore enforcement.
func (o *DeviceObserver) Reset(ctx context.Context, userID string) {
	o.Admission.Reset(userID)
	snapshots := o.Store.Snapshots()
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].NodeID < snapshots[j].NodeID })
	for _, snapshot := range snapshots {
		if snapshot.ObservationContractVersion != 2 {
			continue
		}
		state, err := o.States.AppliedState(ctx, snapshot.NodeID, snapshot.Revision)
		if err != nil {
			continue
		}
		credentials := entryCredentials(state)
		owners, err := o.Repository.CredentialOwners(ctx, keysOf(credentials))
		if err != nil {
			continue
		}
		for _, account := range snapshot.Accounts {
			if account.AddressesMeasured && credentials[account.CredentialID] == userID && owners[account.CredentialID] == userID {
				o.Admission.Observe(userID, snapshot.NodeID, account.AddressKeys, false)
			}
		}
	}
	o.Changed()
}
func (o *DeviceObserver) View(ctx context.Context, u User) (DevicesOnline, error) {
	v := DevicesOnline{Items: []connections.DeviceView{}, DeviceLimit: u.DeviceLimit, UnsupportedNodes: []string{}}
	if o == nil {
		return v, nil
	}
	v.Items, _, v.OnlineDevices = o.Admission.State(u.ID, u.DeviceLimit)
	nodes := []node.Node{}
	var err error
	for page := 1; ; page++ {
		batch, total, e := o.Nodes.List(ctx, "", page, 200, false)
		if e != nil {
			err = e
			break
		}
		nodes = append(nodes, batch...)
		if len(nodes) >= total {
			break
		}
	}
	if err != nil {
		return v, err
	}
	for _, n := range nodes {
		state, e := o.States.Published(ctx, n.ID)
		holds := false
		if e == nil {
			for _, uid := range entryCredentials(state) {
				if uid == u.ID {
					holds = true
					break
				}
			}
		}
		if !holds {
			continue
		}
		snapshot, fresh := o.Store.Snapshot(n.ID)
		supported := true
		for _, in := range state.Inbounds {
			if !in.Enabled || in.Protocol == "wireguard" || in.Protocol == "forward" {
				continue
			}
			serves := false
			for _, cid := range in.CredentialIDs {
				if entryCredentials(state)[cid] == u.ID {
					serves = true
				}
			}
			if !serves {
				continue
			}
			core := node.SelectedCore(n.Capabilities, in.Core)
			if core == nil || !core.DeviceGateSupported {
				supported = false
				continue
			}
			if n.Capabilities.MultiCoreSupported {
				part, ok := o.Store.CoreSnapshots(n.ID)[core.Name]
				if !ok || !part.DeviceGateSupported {
					supported = false
				}
			}
		}
		if !o.Store.GateSupport(n.ID) || !supported || !fresh || snapshot.ObservationContractVersion != 2 || !snapshot.DeviceGateSupported {
			v.UnsupportedNodes = append(v.UnsupportedNodes, n.Name)
		}
	}
	sort.Strings(v.UnsupportedNodes)
	v.Enforced = len(v.UnsupportedNodes) == 0
	return v, nil
}
func (o *DeviceObserver) Command(ctx context.Context, nodeID string) (*pb.DeviceGateCommand, error) {
	state, err := o.States.Published(ctx, nodeID)
	if err != nil {
		return nil, nil
	}
	r := devicegate.Request{TTLSeconds: 180, Entries: []devicegate.Entry{}}
	snapshot, fresh := o.Store.Snapshot(nodeID)
	if !fresh || !snapshot.DeviceGateSupported {
		data, _ := json.Marshal(r)
		return &pb.DeviceGateCommand{RequestJson: data}, nil
	}
	credentials := entryCredentials(state)
	for _, cid := range keysOf(credentials) {
		uid := credentials[cid]
		u, err := o.Repository.Get(ctx, uid)
		if err != nil {
			continue
		}
		_, gates, _ := o.Admission.State(uid, u.DeviceLimit)
		if len(gates) > 0 {
			r.Entries = append(r.Entries, devicegate.Entry{CredentialID: cid, Keys: gates})
			o.alert(uid)
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return &pb.DeviceGateCommand{RequestJson: data}, nil
}

// Subscribe carries only an invalidation signal, never address material.
func (o *DeviceObserver) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	o.mu.Lock()
	if o.subscribers == nil {
		o.subscribers = map[chan struct{}]bool{}
	}
	o.subscribers[ch] = true
	o.mu.Unlock()
	return ch, func() { o.mu.Lock(); delete(o.subscribers, ch); o.mu.Unlock() }
}
func (o *DeviceObserver) Changed() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for ch := range o.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (o *DeviceObserver) alert(userID string) {
	now := time.Now()
	if o.Now != nil {
		now = o.Now()
	}
	o.mu.Lock()
	if o.alerted == nil {
		o.alerted = map[string]time.Time{}
	}
	last := o.alerted[userID]
	if !last.IsZero() && now.Sub(last) < time.Hour {
		o.mu.Unlock()
		return
	}
	o.alerted[userID] = now
	o.mu.Unlock()
	if o.Publish != nil {
		o.Publish("system.alert", map[string]any{"category": "user_access", "userId": userID, "message": "同时在线设备上限已触发"})
	}
}
