package proxy

import (
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOutboundInputOfCopiesWritableFields(t *testing.T) {
	outbound := Outbound{Settings: desiredstate.ProtocolSettings{Transport: "ws", ServerName: "relay.example.com", Path: "/relay"}}
	stored := reflect.ValueOf(&outbound).Elem()
	inputType := reflect.TypeOf(OutboundInput{})
	for index := 0; index < inputType.NumField(); index++ {
		name := inputType.Field(index).Name
		field := stored.FieldByName(name)
		if !field.IsValid() {
			continue // Username and Secret are writes, never stored plaintext fields.
		}
		switch field.Kind() {
		case reflect.String:
			field.SetString("copy " + name)
		case reflect.Bool:
			field.SetBool(true)
		case reflect.Int:
			field.SetInt(int64(index + 1))
		default:
			if field.IsZero() {
				t.Fatalf("add a nonzero fixture for writable field %s", name)
			}
		}
	}
	input := outboundInputOf(outbound)
	got := reflect.ValueOf(input)
	for index := 0; index < inputType.NumField(); index++ {
		name := inputType.Field(index).Name
		if want := stored.FieldByName(name); want.IsValid() && !reflect.DeepEqual(got.Field(index).Interface(), want.Interface()) {
			t.Errorf("writable field %s was not copied", name)
		}
	}
	if input.Username != "" || input.Secret != "" {
		t.Fatal("copying an outbound must retain its existing credentials")
	}
}

// The canvas owns the relay route, while the Owner's source binding, probe and
// backup selection belong to the generated outbound and must survive reuse.
func canvasOutboundWithOwnerSettings(t *testing.T) (*publishPanel, CanvasScheme, Outbound) {
	t.Helper()
	p := newPublishPanel(t)
	n, err := p.service.Nodes.Get(p.ctx, p.nodeA)
	if err != nil {
		t.Fatal(err)
	}
	n.Capabilities.OutboundSourceSupported = true
	if _, err := (&node.Repository{Database: p.database}).Record(p.ctx, p.nodeA, &pb.AgentMessage{Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: n.Capabilities}}); err != nil {
		t.Fatal(err)
	}
	connection := canvasTestHop("i-a", "i-b", 8443)
	connection.TunnelProtocol = "wireguard"
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{connection})
	// Selecting an exit inbound outside TLS mode uses that inbound's protocol
	// for the generated relay. SOCKS5 isolates outbound preservation from TLS
	// material regeneration by the inbound save path.
	exit, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "SOCKS5 exit", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1081, Enabled: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	layout.Instances[1].Ref = "inbound:" + exit.ID
	layout.Instances[1].Role = "exit"
	scheme, _, err := p.publishScheme(t, "preserve outbound", layout)
	if err != nil {
		t.Fatal(err)
	}
	_, outbounds, _ := p.publishedObjects(t, scheme.Name)
	var previous Outbound
	for _, outbound := range outbounds {
		if outbound.NodeID == p.nodeA {
			previous = outbound
		}
	}
	if previous.ID == "" {
		t.Fatal("generated source outbound missing")
	}
	if previous.Protocol != "socks5" {
		t.Fatalf("expected the selected non-TLS exit protocol, got %s", previous.Protocol)
	}
	backup, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "Owner backup", NodeID: p.nodeA, Protocol: "direct", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.service.SaveOutbound(p.ctx, previous.ID, OutboundInput{
		Name: previous.Name, NodeID: previous.NodeID, Protocol: previous.Protocol,
		Enabled: true, TargetInboundID: previous.TargetInboundID, SupportTCP: true, SupportUDP: true,
		FailurePolicy: "BackupOutbound", BackupOutboundID: backup.ID,
		SourceIP: "192.0.2.25", SourceInterface: "eth1", ProbeURL: "https://probe.example.com/health", ProbeInterval: 45,
	})
	if err != nil {
		t.Fatal(err)
	}
	previous, err = p.service.Repository.Outbound(p.ctx, previous.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveInbound(p.ctx, p.inbound.ID, inboundInputOf(p.inbound, "direct")); err != nil {
		t.Fatal(err)
	}
	return p, scheme, previous
}

func TestCanvasRepublicationPreservesOutboundOwnerSettings(t *testing.T) {
	p, scheme, previous := canvasOutboundWithOwnerSettings(t)
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if p.deployer.calls != 2 {
		t.Fatalf("publication skipped the changed outbound: calls=%d", p.deployer.calls)
	}
	got, err := p.service.Repository.Outbound(p.ctx, previous.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	want := previous
	want.FailurePolicy = "Block" // The canvas owns this field.
	if !reflect.DeepEqual(got, want) {
		logOutboundDifferences(t, got, want)
		t.Fatalf("republication changed Owner settings: source=%q/%q, probe=%q/%d, backup=%q; want source=%q/%q, probe=%q/%d, backup=%q", got.SourceIP, got.SourceInterface, got.ProbeURL, got.ProbeInterval, got.BackupOutboundID, want.SourceIP, want.SourceInterface, want.ProbeURL, want.ProbeInterval, want.BackupOutboundID)
	}
}

func TestCanvasFailedRepublicationRestoresAllOutboundFields(t *testing.T) {
	p, scheme, previous := canvasOutboundWithOwnerSettings(t)
	// The entry route is saved after the generated outbounds. A database refusal
	// exercises compensation after the source outbound has already changed.
	if _, err := p.database.ExecContext(p.ctx, `CREATE TRIGGER refuse_canvas_entry_update BEFORE UPDATE ON inbounds WHEN OLD.port=1080 BEGIN SELECT RAISE(ABORT, 'refused canvas entry update'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "refused canvas entry update") || strings.Contains(err.Error(), "撤销本次修改时出错") {
		t.Fatalf("expected entry update failure with successful compensation, got %v", err)
	}
	if p.deployer.calls != 1 {
		t.Fatalf("failed publication reached deployment: calls=%d", p.deployer.calls)
	}
	got, err := p.service.Repository.Outbound(p.ctx, previous.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, previous) {
		logOutboundDifferences(t, got, previous)
		t.Fatalf("compensation changed outbound fields: source=%q/%q, probe=%q/%d, backup=%q, policy=%q; want source=%q/%q, probe=%q/%d, backup=%q, policy=%q", got.SourceIP, got.SourceInterface, got.ProbeURL, got.ProbeInterval, got.BackupOutboundID, got.FailurePolicy, previous.SourceIP, previous.SourceInterface, previous.ProbeURL, previous.ProbeInterval, previous.BackupOutboundID, previous.FailurePolicy)
	}
}

func logOutboundDifferences(t *testing.T, got, want Outbound) {
	t.Helper()
	actual, expected := reflect.ValueOf(got), reflect.ValueOf(want)
	for index := 0; index < actual.NumField(); index++ {
		if !reflect.DeepEqual(actual.Field(index).Interface(), expected.Field(index).Interface()) {
			t.Logf("different outbound field: %s", actual.Type().Field(index).Name)
		}
	}
	a, b := reflect.ValueOf(got.Settings), reflect.ValueOf(want.Settings)
	for index := 0; index < a.NumField(); index++ {
		if !reflect.DeepEqual(a.Field(index).Interface(), b.Field(index).Interface()) {
			t.Logf("different protocol field: %s", a.Type().Field(index).Name)
		}
	}
}
