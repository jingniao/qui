package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	h.applyResult(result)
	return result
}

// publishScheme publishes a saved drawing through the canvas and reports every
// node applied its revision.
func (h *gostHosts) publishScheme(scheme proxy.CanvasScheme) proxy.CanvasPublishResult {
	h.t.Helper()
	published, err := h.service.PublishCanvasScheme(h.ctx, scheme.ID, "Owner", h.users)
	if err != nil {
		h.t.Fatalf("the publication was refused: %v", err)
	}
	h.applyResult(published)
	return published
}

func (h *gostHosts) applyResult(result proxy.CanvasPublishResult) {
	h.t.Helper()
	for _, applied := range result.Nodes {
		if err := h.repository.Result(h.ctx, applied.NodeID, &pb.ApplyResult{Revision: applied.Revision, Success: true}); err != nil {
			h.t.Fatalf("%s: %v", applied.NodeID, err)
		}
	}
}

func nodeOrder(published proxy.CanvasPublishResult) []string {
	order := []string{}
	for _, applied := range published.Nodes {
		order = append(order, applied.NodeID)
	}
	return order
}

// gostTunnelScheme prepares uk's allocation range and saves the drawing the fix
// was reported from: us2 forwards through a GOST tunnel to uk's relay listener.
func (h *gostHosts) gostTunnelScheme(name string, targetPort int) proxy.CanvasScheme {
	h.t.Helper()
	if _, err := h.database.ExecContext(h.ctx, "INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,23000,23010)", h.uk); err != nil {
		h.t.Fatal(err)
	}
	layout := proxy.CanvasLayout{
		Instances: []proxy.CanvasInstance{
			{InstanceID: "i-entry", Ref: "server:" + h.us2, X: 40, Y: 120, Role: "entry"},
			{InstanceID: "i-exit", Ref: "server:" + h.uk, X: 320, Y: 120, Role: "exit"},
		},
		Connections: []proxy.CanvasConnection{{ID: "gost-hop", Source: "i-entry", Target: "i-exit", Type: "port_forward", TunnelProtocol: "gost",
			ListenPort: portValue(23200), TargetHost: "192.0.2.99", TargetPort: portValue(targetPort), SupportTCP: true, SupportUDP: true}},
	}
	// Seed the stored legacy drawing; C1 only publishes existing GOST forwards.
	scheme, err := h.service.Repository.SaveCanvasScheme(h.ctx, "", proxy.CanvasSchemeInput{Name: name, Execution: "server", Layout: layout})
	if err != nil {
		h.t.Fatal(err)
	}
	return scheme
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

// revision reports the revision a node currently runs.
func (h *gostHosts) revision(nodeID string) int64 {
	h.t.Helper()
	var revision int64
	if err := h.database.QueryRowContext(h.ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", nodeID).Scan(&revision); err != nil {
		h.t.Fatal(err)
	}
	return revision
}

// targets counts the deployment targets a node has: a publication that skips a
// node creates none for it.
func (h *gostHosts) targets(nodeID string) int {
	h.t.Helper()
	var count int
	if err := h.database.QueryRowContext(h.ctx, "SELECT count(*) FROM deployment_targets WHERE node_id=?", nodeID).Scan(&count); err != nil {
		h.t.Fatal(err)
	}
	return count
}

// TestGostTunnelPublishesOverExistingOrdinaryRelays is the host case: alpine → uk
// (shadowsocks) and uk → us2 (vless) already run, so the enabled outbound on
// us2's future GOST exit asks for uk after us2 while the GOST tunnel asks for the
// exit before the entry. Only the drawing's own dependencies decide the batch
// (uk, us2), the ordinary relays and their chain stay untouched, and alpine -
// captured only because the drawing reaches it through uk - keeps running: its
// configuration did not change, so it gets no revision and no deployment target.
func TestGostTunnelPublishesOverExistingOrdinaryRelays(t *testing.T) {
	hosts := newGostHosts(t)
	chain := hosts.saveOrdinaryRelays()
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	// The explicit relay batch keeps every member, even one that already runs the
	// captured state: only the canvas drawing prunes.
	again := hosts.publish("ordinary relays again", ordinary)
	if again.BatchID == "" || len(again.Nodes) != 3 {
		t.Fatalf("the explicit relay batch skipped a member: %+v", again)
	}
	alpineRevision := hosts.revision(hosts.alpine)
	alpineTargets := hosts.targets(hosts.alpine)
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
	scheme, err := hosts.service.Repository.SaveCanvasScheme(hosts.ctx, "", proxy.CanvasSchemeInput{Name: "gost us2 uk", Execution: "server", Layout: layout})
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
	if len(order) != 2 || order[0] != hosts.uk || order[1] != hosts.us2 {
		t.Fatalf("publication order %v is not uk, us2", order)
	}
	batch, err := hosts.repository.Batch(hosts.ctx, published.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != "success" || len(batch.Items) != 2 {
		t.Fatalf("batch %+v", batch)
	}
	if got := hosts.revision(hosts.alpine); got != alpineRevision {
		t.Fatalf("the unchanged relay source was redeployed: revision %d, was %d", got, alpineRevision)
	}
	if got := hosts.targets(hosts.alpine); got != alpineTargets {
		t.Fatalf("the unchanged relay source got %d deployment targets, had %d", got, alpineTargets)
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

// A publication leaves an expansion node alone when the captured state and the
// inbound intent are exactly what it runs, and publishes it for the smallest
// difference. The revision label alone is not a difference.
func TestCanvasPublishSkipsAnUnchangedExpansionNode(t *testing.T) {
	hosts := newGostHosts(t)
	hosts.saveOrdinaryRelays()
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	state, intent, err := hosts.service.SnapshotIntent(hosts.ctx, hosts.us2, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	revision, targets := hosts.revision(hosts.us2), hosts.targets(hosts.us2)
	publisher := &CanvasPublisher{Repository: hosts.repository}
	result, err := publisher.PublishCanvas(hosts.ctx, "方案发布 unchanged", "owner", []proxy.NodeSnapshot{{State: state, InboundIntent: intent, ExpandedOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.BatchID != "" || len(result.Nodes) != 0 {
		t.Fatalf("an unchanged node was published: %+v", result)
	}
	if hosts.revision(hosts.us2) != revision || hosts.targets(hosts.us2) != targets {
		t.Fatal("an unchanged node was redeployed")
	}
	// Another intent is a change: the revision the node runs next inherits it.
	another := append(append([]string{}, intent...), "extra")
	result, err = publisher.PublishCanvas(hosts.ctx, "方案发布 intent", "owner", []proxy.NodeSnapshot{{State: state, InboundIntent: another, ExpandedOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].NodeID != hosts.us2 {
		t.Fatalf("an intent change was skipped: %+v", result)
	}
	hosts.applyResult(result)
	if hosts.revision(hosts.us2) == revision {
		t.Fatal("the changed intent was not published")
	}
	// The same state and intent again differs only in the revision label.
	result, err = publisher.PublishCanvas(hosts.ctx, "方案发布 same", "owner", []proxy.NodeSnapshot{{State: state, InboundIntent: another, ExpandedOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 0 {
		t.Fatalf("a revision-label-only difference was published: %+v", result)
	}
}

// A revision stored before the intent column says nothing about which inbounds
// the Owner wants enabled, so an empty captured set is still a difference.
func TestCanvasPublishTreatsAnUnknownPublishedIntentAsChanged(t *testing.T) {
	hosts := newGostHosts(t)
	hosts.saveOrdinaryRelays()
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	state, intent, err := hosts.service.SnapshotIntent(hosts.ctx, hosts.us2, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	applySnapshot(t, hosts.repository, state)
	if intent == nil {
		t.Fatal("the renderer reported an unknown intent")
	}
	result, err := (&CanvasPublisher{Repository: hosts.repository}).PublishCanvas(hosts.ctx, "方案发布 unknown intent", "owner", []proxy.NodeSnapshot{{State: state, InboundIntent: intent, ExpandedOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("an unknown published intent was treated as unchanged: %+v", result)
	}
}

// The sameDesiredState comparison ignores the revision label and nothing else.
func TestSameDesiredStateIgnoresTheRevisionLabel(t *testing.T) {
	state := canvasState("node-compare", 2500)
	relabelled := canvasState("node-compare", 2500)
	relabelled.Revision = 77
	if !sameDesiredState(state, relabelled) {
		t.Fatal("the revision label counted as a difference")
	}
	relabelled.Inbounds[0].Port = 2501
	if sameDesiredState(state, relabelled) {
		t.Fatal("another port compared equal")
	}
}

// The users, the credentials, an inbound's credential bindings and its WireGuard
// peers are sets. An automatic access revision rebuilds them from the published
// order and appends what came back - a user disabled and then re-enabled ends up
// behind the others - while a fresh render emits them in the order it reads the
// access data. Neither order is a configuration change, so the comparison must not
// turn it into a needless redeploy. The comparison works on copies: the states
// handed in keep the order the caller built.
func TestSameDesiredStateIgnoresTheOrderOfTheUserAccessSets(t *testing.T) {
	build := func(order []string) desiredstate.State {
		state := canvasState("node-compare", 2600)
		inbound := desiredstate.Inbound{
			ID: "wg-in", Protocol: "wireguard", Listen: "0.0.0.0", Port: 2601, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{},
			Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: "server-secret", Addresses: []string{"10.10.0.1/24"}, Peers: []desiredstate.WireGuardPeer{}}},
		}
		for _, id := range order {
			state.Users = append(state.Users, desiredstate.User{ID: id, Enabled: true})
			state.Credentials = append(state.Credentials, desiredstate.Credential{ID: "cred-" + id, UserID: id, DeviceID: "device-" + id, Protocol: "wireguard", Secret: "secret-" + id})
			inbound.CredentialIDs = append(inbound.CredentialIDs, "cred-"+id)
			inbound.Settings.WireGuard.Peers = append(inbound.Settings.WireGuard.Peers, desiredstate.WireGuardPeer{PublicKey: "public-" + id, AllowedIPs: []string{"10.10.0.2/32"}, CredentialID: "cred-" + id})
		}
		state.Inbounds = append(state.Inbounds, inbound)
		return state
	}
	published := build([]string{"user-b", "user-a"})
	fresh := build([]string{"user-a", "user-b"})
	if !sameDesiredState(published, fresh) {
		t.Fatal("the same user access sets in another order counted as a change")
	}
	if published.Users[0].ID != "user-b" || published.Credentials[0].ID != "cred-user-b" || published.Inbounds[1].CredentialIDs[0] != "cred-user-b" || published.Inbounds[1].Settings.WireGuard.Peers[0].CredentialID != "cred-user-b" {
		t.Fatalf("the first state was reordered: %+v", published.Users)
	}
	if fresh.Users[0].ID != "user-a" || fresh.Credentials[0].ID != "cred-user-a" || fresh.Inbounds[1].CredentialIDs[0] != "cred-user-a" || fresh.Inbounds[1].Settings.WireGuard.Peers[0].CredentialID != "cred-user-a" {
		t.Fatalf("the second state was reordered: %+v", fresh.Users)
	}
	changed := build([]string{"user-a", "user-b"})
	changed.Credentials[0].Secret = "another-secret"
	if sameDesiredState(published, changed) {
		t.Fatal("a different credential secret compared equal")
	}
	changed = build([]string{"user-a", "user-b"})
	changed.Inbounds[1].Settings.WireGuard.Peers[0].PublicKey = "another-public-key"
	if sameDesiredState(published, changed) {
		t.Fatal("a different WireGuard peer compared equal")
	}
}

// An empty set has two representations, and the two sides of the comparison do
// not pick the same one. An automatic access revision rebuilds the sets from the
// published ones, so disabling the last user of a WireGuard inbound publishes an
// allocated but empty list (peers: []); a fresh render emits nothing at all,
// which encodes as null. That is the same empty set and must not redeploy the
// node, while a set that gained a member is still a change.
func TestSameDesiredStateTreatsAnEmptyUserAccessSetAsNil(t *testing.T) {
	build := func(allocated bool) desiredstate.State {
		state := canvasState("node-compare", 2700)
		ids, peers := []string(nil), []desiredstate.WireGuardPeer(nil)
		if allocated {
			state.Users = []desiredstate.User{}
			state.Credentials = []desiredstate.Credential{}
			ids, peers = []string{}, []desiredstate.WireGuardPeer{}
		}
		state.Inbounds = append(state.Inbounds, desiredstate.Inbound{
			ID: "wg-in", Protocol: "wireguard", Listen: "0.0.0.0", Port: 2701, DefaultOutbound: "direct", CredentialIDs: ids,
			Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: "server-secret", Addresses: []string{"10.10.0.1/24"}, Peers: peers}},
		})
		return state
	}
	fresh := build(false)
	automatic := build(true)
	if !sameDesiredState(fresh, automatic) {
		t.Fatal("an empty user access set counted as a change in its representation")
	}
	changes := []struct {
		name   string
		change func(state *desiredstate.State)
	}{
		{"user", func(state *desiredstate.State) {
			state.Users = []desiredstate.User{{ID: "user-a", Enabled: true}}
		}},
		{"credential", func(state *desiredstate.State) {
			state.Credentials = []desiredstate.Credential{{ID: "cred-user-a", UserID: "user-a", DeviceID: "device-a", Protocol: "wireguard", Secret: "secret-a"}}
		}},
		{"credential ids", func(state *desiredstate.State) {
			state.Inbounds[1].CredentialIDs = []string{"cred-user-a"}
		}},
		{"peer", func(state *desiredstate.State) {
			state.Inbounds[1].Settings.WireGuard.Peers = []desiredstate.WireGuardPeer{{PublicKey: "public-a", AllowedIPs: []string{"10.10.0.2/32"}, CredentialID: "cred-user-a"}}
		}},
	}
	for _, item := range changes {
		changed := build(false)
		item.change(&changed)
		if sameDesiredState(fresh, changed) {
			t.Fatalf("a non-empty %s compared equal to an empty set", item.name)
		}
	}
}

// A node the drawing only reaches through a relay is published when its own
// configuration changed while it was unpublished: the captured state is the
// Owner's edit, and skipping it would leave the node behind.
func TestCanvasPublishIncludesAnExpansionNodeWithItsOwnChange(t *testing.T) {
	hosts := newGostHosts(t)
	hosts.saveOrdinaryRelays()
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	if _, err := hosts.service.SaveInbound(hosts.ctx, "", proxy.InboundInput{Name: "alpine entry", NodeID: hosts.alpine, Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 35310, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	before := hosts.revision(hosts.alpine)
	scheme := hosts.gostTunnelScheme("gost us2 uk own change", 8443)
	order := nodeOrder(hosts.publishScheme(scheme))
	if len(order) != 3 || order[0] != hosts.uk || order[1] != hosts.us2 || order[2] != hosts.alpine {
		t.Fatalf("publication order %v is not uk, us2, alpine", order)
	}
	if hosts.revision(hosts.alpine) == before {
		t.Fatal("the unpublished change was not published with the drawing")
	}
}

// An inbound without user credentials renders disabled whichever way the Owner
// set it, so only the captured intent differs. The node is published anyway,
// because the intent is what an automatic quota revision inherits.
func TestCanvasPublishIncludesAnExpansionNodeWithOnlyItsIntentChanged(t *testing.T) {
	hosts := newGostHosts(t)
	hosts.saveOrdinaryRelays()
	spare, err := hosts.service.SaveInbound(hosts.ctx, "", proxy.InboundInput{Name: "alpine spare", NodeID: hosts.alpine, Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 35320, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	if _, err := hosts.service.SaveInbound(hosts.ctx, spare.ID, proxy.InboundInput{Name: "alpine spare", NodeID: hosts.alpine, Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 35320, Enabled: false, SubscriptionVisible: true, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	state, intent, err := hosts.service.SnapshotIntent(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	if len(intent) != 0 {
		t.Fatalf("the disabled inbound stayed in the intent: %v", intent)
	}
	published, _, err := hosts.repository.PublishedWithIntent(hosts.ctx, hosts.alpine)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDesiredState(published, state) {
		t.Fatal("the fixture changed more than the intent")
	}
	before := hosts.revision(hosts.alpine)
	scheme := hosts.gostTunnelScheme("gost us2 uk intent", 8443)
	order := nodeOrder(hosts.publishScheme(scheme))
	if len(order) != 3 || order[2] != hosts.alpine {
		t.Fatalf("publication order %v is not uk, us2, alpine", order)
	}
	if hosts.revision(hosts.alpine) == before {
		t.Fatal("the changed intent was not published with the drawing")
	}
}

// The whole captured set is checked before anything is skipped: uk itself did not
// change, but its outbound still dials the relay listener us2 moved, so the
// drawing cannot be published even though uk would not be redeployed.
func TestCanvasPublishValidatesTheWholeCapturedSetBeforeSkipping(t *testing.T) {
	hosts := newGostHosts(t)
	hosts.saveOrdinaryRelays()
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	ukState, ukIntent, err := hosts.service.SnapshotIntent(hosts.ctx, hosts.uk, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	us2State, us2Intent, err := hosts.service.SnapshotIntent(hosts.ctx, hosts.us2, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	moved := false
	for index := range us2State.Inbounds {
		if us2State.Inbounds[index].Port == 35210 {
			us2State.Inbounds[index].Port, moved = 35211, true
		}
	}
	if !moved {
		t.Fatal("the fixture has no us2 relay listener")
	}
	_, err = (&CanvasPublisher{Repository: hosts.repository}).PublishCanvas(hosts.ctx, "方案发布 stale source", "owner", []proxy.NodeSnapshot{
		{State: ukState, InboundIntent: ukIntent, ExpandedOnly: true},
		{State: us2State, InboundIntent: us2Intent},
	})
	if !errors.Is(err, ErrDependency) {
		t.Fatalf("a skipped source whose target moved was accepted: %v", err)
	}
}

// Pruning can leave a single node, and then the publication is that node's own
// revision: a batch needs two.
func TestCanvasPublishPrunedToOneNodeUsesASingleRevision(t *testing.T) {
	hosts := newGostHosts(t)
	hosts.saveOrdinaryRelays()
	ordinary, err := hosts.service.RelaySnapshots(hosts.ctx, hosts.alpine, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	hosts.publish("ordinary relays", ordinary)
	ukState, ukIntent, err := hosts.service.SnapshotIntent(hosts.ctx, hosts.uk, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	us2State, us2Intent, err := hosts.service.SnapshotIntent(hosts.ctx, hosts.us2, hosts.users)
	if err != nil {
		t.Fatal(err)
	}
	ukState.Inbounds[0].Port = 35111
	before := hosts.revision(hosts.us2)
	result, err := (&CanvasPublisher{Repository: hosts.repository}).PublishCanvas(hosts.ctx, "方案发布 one node", "owner", []proxy.NodeSnapshot{
		{State: us2State, InboundIntent: us2Intent, ExpandedOnly: true},
		{State: ukState, InboundIntent: ukIntent},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BatchID != "" || len(result.Nodes) != 1 || result.Nodes[0].NodeID != hosts.uk {
		t.Fatalf("single-node publication %+v", result)
	}
	if hosts.revision(hosts.us2) != before {
		t.Fatal("the skipped relay target was redeployed")
	}
}
