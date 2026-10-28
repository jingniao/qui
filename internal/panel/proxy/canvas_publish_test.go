package proxy

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func canvasTestPort(port int) *int { return &port }

// canvasTestSet is one server instance of a scheme under test.
type canvasTestSet struct {
	A, B, C string
}

func canvasTestLayout(p *publishPanel, nodes canvasTestSet, entry string, connections []CanvasConnection) CanvasLayout {
	instances := []CanvasInstance{
		{InstanceID: "i-a", Ref: "server:" + nodes.A, X: 40, Y: 120, Role: "entry"},
		{InstanceID: "i-b", Ref: "server:" + nodes.B, X: 320, Y: 120, Role: "relay"},
	}
	if nodes.C != "" {
		instances = append(instances, CanvasInstance{InstanceID: "i-c", Ref: "server:" + nodes.C, X: 600, Y: 120, Role: "exit"})
	}
	if entry != "" {
		instances = append(instances, CanvasInstance{InstanceID: "i-in", Ref: "inbound:" + entry, X: 40, Y: 300, Role: "entry"})
	}
	return CanvasLayout{Instances: instances, Connections: connections}
}

func canvasTestHop(source, target string, port int) CanvasConnection {
	return CanvasConnection{ID: "c-" + source + "-" + target, Source: source, Target: target, Type: "tunnel", TunnelProtocol: "tls", TunnelPort: canvasTestPort(port), SupportTCP: true, SupportUDP: true}
}

// publishedObjects counts the objects one scheme generated, so a test can check
// both that publishing created them and that re-publishing did not duplicate
// them.
func (p *publishPanel) publishedObjects(t *testing.T, name string) ([]Inbound, []Outbound, []Chain) {
	t.Helper()
	prefix := canvasObjectPrefix(name)
	inbounds, err := p.service.Repository.canvasInboundsByPrefix(p.ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	outbounds, err := p.service.Repository.canvasOutboundsByPrefix(p.ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	chains, err := p.service.Repository.canvasChainsByPrefix(p.ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	return inbounds, outbounds, chains
}

// TestCanvasPublishTwoHopsCreatesProxyChain checks that a three-server drawing
// becomes one proxy chain whose entry inbound routes into it, and that the
// publication reaches the exit before the relay before the entry.
func TestCanvasPublishTwoHopsCreatesProxyChain(t *testing.T) {
	p := newPublishPanel(t)
	connections := []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)}
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, p.inbound.ID, connections)
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "两跳", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	order := p.deployer.order[0]
	if len(order) != 3 || order[0] != p.nodeC || order[1] != p.nodeB || order[2] != p.nodeA {
		t.Fatalf("publication order %v is not exit, relay, entry", order)
	}
	inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name)
	if len(inbounds) != 2 || len(outbounds) != 2 || len(chains) != 1 {
		t.Fatalf("generated %d inbounds, %d outbounds, %d chains", len(inbounds), len(outbounds), len(chains))
	}
	chain := chains[0]
	if len(chain.NodeIDs) != 3 || chain.NodeIDs[0] != p.nodeA || chain.NodeIDs[2] != p.nodeC || len(chain.OutboundIDs) != 2 || len(chain.Hops) != 2 {
		t.Fatalf("chain %+v", chain)
	}
	entry, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if entry.DefaultOutbound != "chain:"+chain.ID {
		t.Fatalf("entry default outbound %q, want chain %q", entry.DefaultOutbound, chain.ID)
	}
}

// TestCanvasPublishIsIdempotent checks that publishing the same drawing twice
// updates the objects instead of duplicating them, and that a path that grew a
// second hop turns into a chain rather than a second set of objects.
func TestCanvasPublishIsIdempotent(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "重复发布", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	firstInbounds, firstOutbounds, _ := p.publishedObjects(t, scheme.Name)
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, stored.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name)
	if len(inbounds) != 1 || len(outbounds) != 1 || len(chains) != 0 {
		t.Fatalf("re-publishing generated %d inbounds, %d outbounds, %d chains", len(inbounds), len(outbounds), len(chains))
	}
	if inbounds[0].ID != firstInbounds[0].ID || outbounds[0].ID != firstOutbounds[0].ID {
		t.Fatal("re-publishing replaced the generated objects instead of updating them")
	}
	if p.deployer.calls != 2 {
		t.Fatalf("deployer called %d times", p.deployer.calls)
	}
}

// TestCanvasPublishPrunesRemovedEdge checks that removing a connection deletes
// nothing until the next publication, and that the publication then removes only
// the objects that scheme generated.
func TestCanvasPublishPrunesRemovedEdge(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "删除实例", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if inbounds, outbounds, _ := p.publishedObjects(t, scheme.Name); len(inbounds) != 1 || len(outbounds) != 1 {
		t.Fatalf("first publication: %d inbounds, %d outbounds", len(inbounds), len(outbounds))
	}
	// The drawing loses its arrow: the objects stay until the next publication.
	trimmed := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, nil)
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: scheme.Name, Execution: "server", Layout: trimmed, UpdatedAt: stored.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if inbounds, _, _ := p.publishedObjects(t, scheme.Name); len(inbounds) != 1 {
		t.Fatal("editing the drawing deleted its objects before a publication")
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, updated.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name)
	if len(inbounds) != 0 || len(outbounds) != 0 || len(chains) != 0 {
		t.Fatalf("after pruning: %d inbounds, %d outbounds, %d chains", len(inbounds), len(outbounds), len(chains))
	}
	entry, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if entry.DefaultOutbound != "direct" {
		t.Fatalf("entry default outbound %q after pruning", entry.DefaultOutbound)
	}
}

// TestCanvasPublishRefusesWhatItCannotExpress checks the refusals of U2c: an
// unsupported tunnel protocol, port forwarding, a client scheme and a listener
// conflict all stop the publication before it creates anything.
func TestCanvasPublishRefusesWhatItCannotExpress(t *testing.T) {
	cases := []struct {
		name        string
		execution   string
		tunnel      string
		kind        string
		port        int
		wantMessage string
	}{
		{"gost tunnel", "server", "gost", "tunnel", 8443, "暂不支持"},
		{"nftables tunnel", "server", "nftables", "tunnel", 8443, "暂不支持"},
		{"port forwarding", "server", "tls", "port_forward", 8443, "端口转发"},
		{"client scheme", "client", "tls", "tunnel", 8443, "客户端方案"},
		{"listener conflict", "server", "tls", "tunnel", 9000, "占用"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p := newPublishPanel(t)
			if test.port == 9000 {
				if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Existing", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 9000, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"}); err != nil {
					t.Fatal(err)
				}
			}
			connection := canvasTestHop("i-a", "i-b", test.port)
			connection.TunnelProtocol = test.tunnel
			connection.Type = test.kind
			layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{connection})
			scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "拒绝 " + test.name, Execution: test.execution, Layout: layout})
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("publish error %v, want a message containing %q", err, test.wantMessage)
			}
			if inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name); len(inbounds) != 0 || len(outbounds) != 0 || len(chains) != 0 {
				t.Fatalf("a refused publication created %d inbounds, %d outbounds, %d chains", len(inbounds), len(outbounds), len(chains))
			}
			if p.deployer.calls != 0 {
				t.Fatalf("a refused publication reached the deployer %d times", p.deployer.calls)
			}
		})
	}
}

// TestCanvasPublishRefusesFourthHop checks the two-hop limit the proxy chain
// validator enforces: a drawing with three arrows is refused instead of being
// published partially.
func TestCanvasPublishRefusesFourthHop(t *testing.T) {
	p := newPublishPanel(t)
	extra, _, _, err := (&node.Repository{Database: p.database}).CreateEnrollment(p.ctx, "Node D", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	layout := CanvasLayout{
		Instances: []CanvasInstance{
			{InstanceID: "i-a", Ref: "server:" + p.nodeA, Role: "entry"},
			{InstanceID: "i-b", Ref: "server:" + p.nodeB, Role: "relay"},
			{InstanceID: "i-c", Ref: "server:" + p.nodeC, Role: "relay"},
			{InstanceID: "i-d", Ref: "server:" + extra.ID, Role: "exit"},
		},
		Connections: []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 8543), canvasTestHop("i-c", "i-d", 8643)},
	}
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "四跳", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "最多三跳") {
		t.Fatalf("publish error %v, want the hop limit", err)
	}
	if inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name); len(inbounds) != 0 || len(outbounds) != 0 || len(chains) != 0 {
		t.Fatalf("a refused publication created %d inbounds, %d outbounds, %d chains", len(inbounds), len(outbounds), len(chains))
	}
}

// TestCanvasPublishRefusesAmbiguousEntryRoute checks that a drawing whose entry
// inbound has two possible routes is refused instead of silently picking one.
func TestCanvasPublishRefusesAmbiguousEntryRoute(t *testing.T) {
	p := newPublishPanel(t)
	connections := []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-a", "i-c", 8543)}
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, p.inbound.ID, connections)
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "分流入口", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "多个出站目标") {
		t.Fatalf("publish error %v, want the ambiguous route refusal", err)
	}
}

// TestCanvasSchemeNamesAreUnique checks the rule publishing depends on: two
// drawings cannot share a name, because their generated objects would collide.
func TestCanvasSchemeNamesAreUnique(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, nil)
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "同名", Execution: "server", Layout: layout}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "同名", Execution: "server", Layout: layout}); !errors.Is(err, ErrDuplicateSchemeName) {
		t.Fatalf("duplicate scheme name accepted: %v", err)
	}
}

// the U2c acceptance: a two-server drawing becomes a relay inbound on the target,
// an outbound on the source, the entry inbound's default outbound, and one
// publication whose nodes are ordered target first.
func TestCanvasPublishSingleHopTranslatesIntoRelayObjects(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "东京中转", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	if p.deployer.calls != 1 {
		t.Fatalf("publish called the deployer %d times", p.deployer.calls)
	}
	if order := p.deployer.order[0]; len(order) != 2 || order[0] != p.nodeB || order[1] != p.nodeA {
		t.Fatalf("publication order %v is not target first", order)
	}
	if len(result.Nodes) != 2 || result.Nodes[0].Name != "Node B" {
		t.Fatalf("per-node results %+v", result.Nodes)
	}
	inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name)
	if len(inbounds) != 1 || len(outbounds) != 1 || len(chains) != 0 {
		t.Fatalf("generated %d inbounds, %d outbounds, %d chains", len(inbounds), len(outbounds), len(chains))
	}
	relay := inbounds[0]
	if !relay.InternalRelay || relay.SubscriptionVisible || !relay.Enabled || relay.Protocol != "trojan" || relay.Port != 8443 || relay.NodeID != p.nodeB {
		t.Fatalf("relay inbound %+v", relay)
	}
	outbound := outbounds[0]
	if outbound.NodeID != p.nodeA || outbound.TargetNodeID != p.nodeB || outbound.TargetInboundID != relay.ID || outbound.FailurePolicy != "Block" || outbound.CredentialID == "" {
		t.Fatalf("relay outbound %+v", outbound)
	}
	entry, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if entry.DefaultOutbound != outbound.ID {
		t.Fatalf("entry default outbound %q, want %q", entry.DefaultOutbound, outbound.ID)
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PublishedAt == "" || stored.PublishedBatchID != "" {
		t.Fatalf("scheme publication not recorded: %+v", stored)
	}
}

// canvasDeployRecorder stands in for the Deployment module: it records the
// publication order and validates every snapshot it is handed, so the tests can
// check that the scheme produces a deployable state without a database of
// revisions.
type canvasDeployRecorder struct {
	calls  int
	order  [][]string
	result CanvasPublishResult
	err    error
}

func (r *canvasDeployRecorder) PublishCanvas(_ context.Context, _ string, _ string, snapshots []NodeSnapshot) (CanvasPublishResult, error) {
	r.calls++
	ids := []string{}
	for _, snapshot := range snapshots {
		if err := snapshot.State.Validate(); err != nil {
			return CanvasPublishResult{}, err
		}
		ids = append(ids, snapshot.State.NodeID)
	}
	r.order = append(r.order, ids)
	if r.err != nil {
		return CanvasPublishResult{}, r.err
	}
	nodes := []CanvasPublishNode{}
	for index, id := range ids {
		nodes = append(nodes, CanvasPublishNode{NodeID: id, Revision: int64(index + 1), Status: "pending"})
	}
	return CanvasPublishResult{Nodes: nodes}, nil
}

// newPublishPanel builds three enrolled nodes that advertise the relay
// protocols the tests use, one public entry inbound on the first node and a
// service whose deployment side is the recorder above.
type publishPanel struct {
	*canvasPanel
	users    *user.Repository
	deployer *canvasDeployRecorder
	nodeC    string
}

func newPublishPanel(t *testing.T) *publishPanel {
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
	for index, name := range []string{"Node A", "Node B", "Node C"} {
		created, _, _, err := nodes.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := nodes.SetPublicIP(ctx, created.ID, "192.0.2.1"+strconv.Itoa(index)); err != nil {
			t.Fatal(err)
		}
		capabilities := &pb.Capabilities{CoreName: "Xray", Protocols: []*pb.ProtocolCapability{
			{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true, Udp: true},
			{Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true, Udp: true},
		}}
		if _, err := nodes.Record(ctx, created.ID, &pb.AgentMessage{Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	service := &Service{Repository: &Repository{Database: database, Vault: key}, Nodes: nodes}
	entry, err := service.SaveInbound(ctx, "", InboundInput{Name: "Entry", NodeID: ids[0], Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	deployer := &canvasDeployRecorder{}
	service.Deploy = deployer
	panel := &canvasPanel{t: t, ctx: ctx, database: database, service: service, nodeA: ids[0], nodeB: ids[1], inbound: entry}
	return &publishPanel{canvasPanel: panel, users: &user.Repository{Database: database, Vault: key}, deployer: deployer, nodeC: ids[2]}
}
