package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// gostHosts is the three-host topology this fix was reported from: alpine relays
// to uk with shadowsocks (35110) and uk relays to us2 with vless (35210), both
// ordinary relays under one ALP-UK-US2 chain, and every host also reports GOST
// with the relay contract. Publications go through this module's own repository,
// so the batch validation under test is the real one and not a recorder.
type gostHosts struct {
	t          *testing.T
	ctx        context.Context
	database   *sql.DB
	service    *proxy.Service
	repository *Repository
	users      *user.Repository
	alpine     string
	uk         string
	us2        string
}

func newGostHosts(t *testing.T) *gostHosts {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	ids := []string{}
	for index, name := range []string{"alpine-node", "uk-node", "us2-node"} {
		created, _, _, err := nodes.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := nodes.SetPublicIP(ctx, created.ID, "198.51.100."+strconv.Itoa(index+1)); err != nil {
			t.Fatal(err)
		}
		capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true, ForwardInboundSupported: true,
			Cores: []*pb.CoreCapability{
				{Name: "xray", Version: "26.3.27", Protocols: []*pb.ProtocolCapability{
					{Protocol: "shadowsocks", Inbound: true, Outbound: true, Tcp: true, Udp: true},
					{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true},
				}},
				{Name: "gost", Version: "3.3.2", ForwardInboundSupported: true, GostContractVersion: desiredstate.GostRelayContractVersion,
					Protocols: []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true, Tcp: true, Udp: true}}},
			}}
		raw, err := json.Marshal(capabilities)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), created.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	repository := &Repository{Database: database, Vault: key}
	service := &proxy.Service{Repository: &proxy.Repository{Database: database, Vault: key}, Nodes: nodes, Deploy: &CanvasPublisher{Repository: repository}}
	return &gostHosts{t: t, ctx: ctx, database: database, service: service, repository: repository,
		users: &user.Repository{Database: database, Vault: key}, alpine: ids[0], uk: ids[1], us2: ids[2]}
}

// saveOrdinaryRelays creates the relays and the chain the hosts already run, and
// returns the chain they share.
func (h *gostHosts) saveOrdinaryRelays() proxy.Chain {
	h.t.Helper()
	ukRelay, err := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{Name: "relay uk", NodeID: h.uk, Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 35110, Enabled: true, InternalRelay: true, AllowedUserIDs: []string{}, DefaultOutbound: "direct"})
	if err != nil {
		h.t.Fatal(err)
	}
	us2Relay, err := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{Name: "relay us2", NodeID: h.us2, Protocol: "vless", Listen: "0.0.0.0", Port: 35210, Enabled: true, InternalRelay: true, AllowedUserIDs: []string{}, DefaultOutbound: "direct"})
	if err != nil {
		h.t.Fatal(err)
	}
	alpineToUk, err := h.service.SaveOutbound(h.ctx, "", proxy.OutboundInput{Name: "to uk", NodeID: h.alpine, Protocol: "shadowsocks", Enabled: true, TargetInboundID: ukRelay.ID, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
	if err != nil {
		h.t.Fatal(err)
	}
	ukToUs2, err := h.service.SaveOutbound(h.ctx, "", proxy.OutboundInput{Name: "to us2", NodeID: h.uk, Protocol: "vless", Enabled: true, TargetInboundID: us2Relay.ID, SupportTCP: true, FailurePolicy: "Block"})
	if err != nil {
		h.t.Fatal(err)
	}
	chain, err := h.service.SaveChain(h.ctx, "", proxy.ChainInput{Name: "ALP-UK-US2", NodeIDs: []string{h.alpine, h.uk, h.us2}, OutboundIDs: []string{alpineToUk.ID, ukToUs2.ID}, FinalOutboundID: "direct", SupportTCP: true, Enabled: true, FailurePolicy: "Block"})
	if err != nil {
		h.t.Fatal(err)
	}
	return chain
}

// publish creates the batch of captured snapshots and reports every node applied
// it, which is what the Agents do on the hosts.
func (h *gostHosts) publish(name string, snapshots []proxy.NodeSnapshot) proxy.CanvasPublishResult {
	h.t.Helper()
	result, err := (&CanvasPublisher{Repository: h.repository}).PublishCanvas(h.ctx, name, "owner", snapshots)
	if err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
	for _, applied := range result.Nodes {
		if err := h.repository.Result(h.ctx, applied.NodeID, &pb.ApplyResult{Revision: applied.Revision, Success: true}); err != nil {
			h.t.Fatalf("%s: %v", name, err)
		}
	}
	return result
}

func (h *gostHosts) inbounds(nodeID string) []proxy.Inbound {
	h.t.Helper()
	records, _, err := h.service.Repository.Inbounds(h.ctx, nodeID, 1, 100, true)
	if err != nil {
		h.t.Fatal(err)
	}
	return records
}

func (h *gostHosts) outbounds(nodeID string) []proxy.Outbound {
	h.t.Helper()
	records, _, err := h.service.Repository.Outbounds(h.ctx, nodeID, 1, 100, true)
	if err != nil {
		h.t.Fatal(err)
	}
	return records
}

func portValue(port int) *int { return &port }

// TestGostTunnelPublishesOverExistingOrdinaryRelays is the host case: alpine → uk
// (shadowsocks) and uk → us2 (vless) already run, so the enabled outbound on
// us2's future GOST exit asks for uk after us2 while the GOST tunnel asks for the
// exit before the entry. Only the drawing's own dependencies decide the batch
// (uk, us2, alpine), and the ordinary relays and their chain stay untouched.
func TestGostTunnelPublishesOverExistingOrdinaryRelays(t *testing.T) {
	hosts := newGostHosts(t)
	chain := hosts.saveOrdinaryRelays()
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	// The relay listener of the drawing takes its port from uk's range.
	if _, err := hosts.database.ExecContext(hosts.ctx, "INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,23000,23010)", hosts.uk); err != nil {
		t.Fatal(err)
	}
	layout := proxy.CanvasLayout{
		Instances: []proxy.CanvasInstance{
			{InstanceID: "i-entry", Ref: "server:" + hosts.us2, X: 40, Y: 120, Role: "entry"},
			{InstanceID: "i-exit", Ref: "server:" + hosts.uk, X: 320, Y: 120, Role: "exit"},
		},
		Connections: []proxy.CanvasConnection{{ID: "gost-hop", Source: "i-entry", Target: "i-exit", Type: "port_forward", TunnelProtocol: "gost",
			ListenPort: portValue(23200), TargetHost: "192.0.2.99", TargetPort: portValue(8443), SupportTCP: true, SupportUDP: true}},
	}
	scheme, err := hosts.service.SaveCanvasScheme(hosts.ctx, "", proxy.CanvasSchemeInput{Name: "gost us2 uk", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	// The preview lists both halves the publication creates: the forward on us2
	// and the relay listener on uk.
	preview, err := hosts.service.PlanCanvasScheme(hosts.ctx, scheme.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.OK {
		t.Fatalf("preview %+v", preview)
	}
	forward, listener := false, false
	for _, server := range preview.Servers {
		for _, object := range server.Creates {
			switch {
			case server.NodeID == hosts.us2 && object.Kind == "forward" && object.Port == 23200:
				forward = true
			case server.NodeID == hosts.uk && object.Kind == "forward" && object.Port >= 23000 && object.Port <= 23010:
				listener = true
			}
		}
	}
	if !forward || !listener {
		t.Fatalf("preview creates %+v", preview.Servers)
	}
	published, err := hosts.service.PublishCanvasScheme(hosts.ctx, scheme.ID, "Owner", hosts.users)
	if err != nil {
		t.Fatalf("the GOST publication was refused: %v", err)
	}
	order := []string{}
	for _, applied := range published.Nodes {
		order = append(order, applied.NodeID)
		if err := hosts.repository.Result(hosts.ctx, applied.NodeID, &pb.ApplyResult{Revision: applied.Revision, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	if len(order) != 3 || order[0] != hosts.uk || order[1] != hosts.us2 || order[2] != hosts.alpine {
		t.Fatalf("publication order %v is not uk, us2, alpine", order)
	}
	batch, err := hosts.repository.Batch(hosts.ctx, published.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != "success" || len(batch.Items) != 3 {
		t.Fatalf("batch %+v", batch)
	}
	// The ordinary relays and the chain still run as they were.
	kept := map[string]bool{}
	for _, inbound := range hosts.inbounds(hosts.uk) {
		if inbound.Port == 35110 && inbound.Protocol == "shadowsocks" && inbound.Enabled && inbound.InternalRelay {
			kept["uk relay"] = true
		}
	}
	for _, inbound := range hosts.inbounds(hosts.us2) {
		if inbound.Port == 35210 && inbound.Protocol == "vless" && inbound.Enabled && inbound.InternalRelay {
			kept["us2 relay"] = true
		}
	}
	for _, outbound := range hosts.outbounds(hosts.alpine) {
		if outbound.Protocol == "shadowsocks" && outbound.Enabled && outbound.TargetNodeID == hosts.uk {
			kept["alpine to uk"] = true
		}
	}
	for _, outbound := range hosts.outbounds(hosts.uk) {
		if outbound.Protocol == "vless" && outbound.Enabled && outbound.TargetNodeID == hosts.us2 {
			kept["uk to us2"] = true
		}
	}
	if stored, err := hosts.service.Repository.Chain(hosts.ctx, chain.ID); err != nil || !stored.Enabled || len(stored.Hops) != 2 || len(stored.NodeIDs) != 3 {
		t.Fatalf("chain %+v %v", stored, err)
	} else {
		kept["chain"] = true
	}
	for _, name := range []string{"uk relay", "us2 relay", "alpine to uk", "uk to us2", "chain"} {
		if !kept[name] {
			t.Fatalf("the publication changed %s", name)
		}
	}
	// The entry forward hops through the relay listener the publication created.
	relayPort := 0
	for _, inbound := range hosts.inbounds(hosts.uk) {
		if inbound.Settings.Forward.RelayMode() {
			relayPort = inbound.Port
		}
	}
	if relayPort < 23000 || relayPort > 23010 {
		t.Fatalf("relay listener port %d is outside uk's range", relayPort)
	}
	entry := proxy.Inbound{}
	for _, inbound := range hosts.inbounds(hosts.us2) {
		if inbound.Protocol == "forward" && len(inbound.Settings.Forward.Hops) > 0 {
			entry = inbound
		}
	}
	if len(entry.Settings.Forward.Hops) != 1 || entry.Settings.Forward.Hops[0].Host != "198.51.100.2" || entry.Settings.Forward.Hops[0].Port != relayPort {
		t.Fatalf("entry hop %+v does not dial the relay listener on %d", entry.Settings.Forward.Hops, relayPort)
	}
}
