package proxy

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCanvasLegacyShadowsocksTLSUpgradeAndUndo(t *testing.T) {
	p, input := quickCanvasFixture(t)
	for _, nodeID := range []string{p.nodeA, p.nodeB} {
		n, err := p.service.Nodes.Get(p.ctx, nodeID)
		if err != nil {
			t.Fatal(err)
		}
		n.Capabilities.Protocols = append(n.Capabilities.Protocols, &pb.ProtocolCapability{Protocol: "shadowsocks", Inbound: true, Outbound: true, Tcp: true, Udp: true})
		data, err := json.Marshal(n.Capabilities)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(data), nodeID); err != nil {
			t.Fatal(err)
		}
	}
	// Reproduce the old planner's actual output before applying the new mapping.
	canvasTunnels["tls"] = "shadowsocks"
	result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	canvasTunnels["tls"] = "trojan"
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
	if err != nil {
		t.Fatal(err)
	}
	oldPlan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	edge := oldPlan.edges[0]
	oldIn, oldOut := edge.adopt, edge.upgradeOutbound
	if oldIn.ID == "" || oldOut.ID == "" {
		t.Fatal("legacy pair not recognized")
	}
	preview, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.OK || len(preview.Conflicts) > 0 {
		t.Fatalf("upgrade conflict: %+v", preview)
	}
	updates := 0
	for _, server := range preview.Servers {
		if len(server.Creates) > 0 || len(server.Removes) > 0 {
			t.Fatalf("upgrade replaced identities: %+v", server)
		}
		for _, o := range server.Updates {
			if (o.Kind == "inbound" || o.Kind == "outbound") && o.Protocol == "trojan" {
				updates++
			}
		}
	}
	if updates != 2 {
		t.Fatalf("Trojan updates=%d", updates)
	}
	unchanged, err := p.service.canvasPublicationUnchanged(p.ctx, oldPlan)
	if err != nil || unchanged {
		t.Fatalf("protocol upgrade skipped: %v %v", unchanged, err)
	}
	undo := &canvasUndo{}
	if _, err = p.service.canvasApplyPlanWithUndo(p.ctx, oldPlan, undo); err != nil {
		t.Fatal(err)
	}
	in, err := p.service.Repository.Inbound(p.ctx, oldIn.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.service.Repository.Outbound(p.ctx, oldOut.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if in.Protocol != "trojan" || in.Port != oldIn.Port || in.Settings.Security != "tls" || out.Protocol != "trojan" || out.TargetInboundID != oldIn.ID {
		t.Fatal("upgrade did not preserve listener/target identity with TLS")
	}
	if err = undo.run(p.ctx); err != nil {
		t.Fatal(err)
	}
	in, err = p.service.Repository.Inbound(p.ctx, oldIn.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	out, err = p.service.Repository.Outbound(p.ctx, oldOut.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if in.Protocol != oldIn.Protocol || in.Name != oldIn.Name || in.RelayCredentialID != oldIn.RelayCredentialID || !reflect.DeepEqual(in.Settings, oldIn.Settings) || out.Protocol != oldOut.Protocol || out.Name != oldOut.Name || !reflect.DeepEqual(out.Settings, oldOut.Settings) {
		t.Fatal("undo failed to restore old objects/material")
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.deployer.order[len(p.deployer.order)-1], []string{p.nodeB, p.nodeA}) {
		t.Fatalf("upgrade order %v", p.deployer.order)
	}
}

func TestQuickCanvasTLSAndUnchangedRemoval(t *testing.T) {
	p, input := quickCanvasFixture(t)
	result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := p.service.Repository.tunnelObjects(p.ctx, scheme.Layout.Connections[0].TunnelID)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if object.Kind == "inbound" {
			in, err := p.service.Repository.Inbound(p.ctx, object.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			if in.Protocol != "trojan" || in.Settings.Security != "tls" || in.Settings.PrivateKeyPEM == "" || in.RelayCredentialID == "" {
				t.Errorf("internal listener is not independent Trojan TLS: protocol=%s security=%s", in.Protocol, in.Settings.Security)
			}
			block, _ := pem.Decode([]byte(in.Settings.CertificatePEM))
			if block == nil {
				t.Fatal("missing certificate")
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			validity := certificate.NotAfter.Sub(certificate.NotBefore)
			if validity < 365*24*time.Hour || validity > 366*24*time.Hour {
				t.Fatalf("internal certificate validity %s", validity)
			}
		}
		if object.Kind == "outbound" {
			out, err := p.service.Repository.Outbound(p.ctx, object.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			if out.Protocol != "trojan" {
				t.Errorf("relay protocol=%s", out.Protocol)
			}
		}
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || len(plan.Entries) != 1 || len(plan.Entries[0].Changes) != 0 {
		t.Fatalf("unchanged removal: %+v", plan.Entries)
	}
	if len(plan.Entries[0].Groups) != 1 {
		t.Fatal("group removal information missing")
	}
}

func TestCanvasExternalExitsRemainUnchanged(t *testing.T) {
	for _, protocol := range []string{"socks5", "http", "shadowsocks", "trojan", "vless"} {
		t.Run(protocol, func(t *testing.T) {
			p := newPublishPanel(t)
			n, err := p.service.Nodes.Get(p.ctx, p.nodeA)
			if err != nil {
				t.Fatal(err)
			}
			n.Capabilities.Protocols = append(n.Capabilities.Protocols, &pb.ProtocolCapability{Protocol: protocol, Inbound: true, Outbound: true, Tcp: true})
			raw, err := json.Marshal(n.Capabilities)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), p.nodeA); err != nil {
				t.Fatal(err)
			}
			settings := desiredstate.ProtocolSettings{}
			secret := "external-secret"
			if protocol == "vless" {
				secret = "00000000-0000-4000-8000-000000000001"
			}
			if protocol == "trojan" {
				settings.Security = "tls"
				settings.ServerName = "example.com"
			}
			if protocol == "shadowsocks" {
				settings.Method = "aes-128-gcm"
			}
			out, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "external", NodeID: p.nodeA, Protocol: protocol, Address: "198.51.100.8", Port: 1080, Enabled: true, Username: "relay", Secret: secret, Settings: settings, SupportTCP: true, FailurePolicy: "Block"})
			if err != nil {
				t.Fatal(err)
			}
			before, err := p.service.Repository.Outbound(p.ctx, out.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "inbound:" + p.inbound.ID, Role: "entry"}, {InstanceID: "exit", Ref: "outbound:" + out.ID, Role: "exit"}}, Connections: []CanvasConnection{canvasTestHop("entry", "exit", 8443)}}
			layout.Connections[0].SupportUDP = false
			scheme := savePlanScheme(t, p, layout)
			plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.edges) != 0 {
				t.Fatal("external exit acquired an internal relay")
			}
			if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			after, err := p.service.Repository.Outbound(p.ctx, out.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("external outbound modified")
			}
		})
	}
}

func TestCanvasTLSUserEntryBaseline(t *testing.T) {
	p, input := quickCanvasFixture(t)
	input.EntryProtocol = "trojan"
	result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || len(plan.Entries[0].Changes) != 0 {
		t.Fatalf("unchanged TLS entry: %v", plan.Entries[0].Changes)
	}
}

func TestCanvasReattachPreservesCreationBaseline(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "recorded", true: "unknown"}[legacy], func(t *testing.T) {
			p, input := quickCanvasFixture(t)
			result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
			if err != nil {
				t.Fatal(err)
			}
			entryID := result.Publication.Entries[0].InboundID
			if legacy {
				if _, err = p.database.Exec("UPDATE canvas_entry_bindings SET creation_config_json=NULL,creation_groups_json=NULL WHERE inbound_id=?", entryID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := p.service.Repository.CanvasEntryBindingByInbound(p.ctx, entryID)
			if err != nil {
				t.Fatal(err)
			}
			if err = p.service.Repository.DeleteCanvasScheme(p.ctx, result.SchemeID); err != nil {
				t.Fatal(err)
			}
			resources, err := p.service.LegacyCanvasResources(p.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(resources.Detached) != 1 {
				t.Fatal("missing detached group")
			}
			group := resources.Detached[0]
			scheme, err := p.service.ReattachCanvasLegacy(p.ctx, CanvasLegacyReattachInput{Name: "reattached", BindingIDs: group.BindingIDs, TunnelIDs: group.TunnelIDs})
			if err != nil {
				t.Fatal(err)
			}
			after, err := p.service.Repository.CanvasEntryBindingByInbound(p.ctx, entryID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.CreationConfigJSON, after.CreationConfigJSON) || !reflect.DeepEqual(before.CreationGroupsJSON, after.CreationGroupsJSON) {
				t.Fatal("reattach replaced baseline")
			}
			plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
			if err != nil {
				t.Fatal(err)
			}
			if plan.OK == legacy {
				t.Fatalf("legacy=%v removal ok=%v changes=%v", legacy, plan.OK, plan.Entries[0].Changes)
			}
		})
	}
}

func TestCanvasBaselineNormalizationAndAuthorization(t *testing.T) {
	p, input := quickCanvasFixture(t)
	result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	in, err := p.service.Repository.Inbound(p.ctx, result.Publication.Entries[0].InboundID, true)
	if err != nil {
		t.Fatal(err)
	}
	in.AllowedUserIDs = []string{"b", "a"}
	first, err := canvasEntryBaseline(in)
	if err != nil {
		t.Fatal(err)
	}
	in.AllowedUserIDs = []string{"a", "b", "a"}
	second, err := canvasEntryBaseline(in)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("allowed user set depends on ordering")
	}
	in.AllowedUserIDs = []string{"a"}
	third, err := canvasEntryBaseline(in)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("authorization edit not detected")
	}
	if strings.Contains(first, "external-secret") || strings.Contains(first, "PRIVATE KEY") {
		t.Fatal("baseline contains plaintext material")
	}
}

func TestCanvasEntryBaselineChanges(t *testing.T) {
	for _, kind := range []string{"port", "settings", "add-group", "remove-group", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			p, input := quickCanvasFixture(t)
			result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
			if err != nil {
				t.Fatal(err)
			}
			scheme, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
			if err != nil {
				t.Fatal(err)
			}
			entryID := result.Publication.Entries[0].InboundID
			want := ""
			switch kind {
			case "port":
				_, err = p.database.Exec("UPDATE inbounds SET port=20009 WHERE id=?", entryID)
				want = "端口"
			case "settings":
				in, e := p.service.Repository.Inbound(p.ctx, entryID, true)
				if e != nil {
					t.Fatal(e)
				}
				in.Settings.Transport = "ws"
				_, err = p.service.Repository.SaveInbound(p.ctx, entryID, inboundInputOf(in, in.DefaultOutbound))
				want = "协议设置"
			case "add-group":
				entryPlanGroup(t, p, "added", "mixed", entryID)
				want = "新增分组"
			case "remove-group":
				_, err = p.database.Exec("DELETE FROM node_group_inbounds WHERE inbound_id=?", entryID)
				want = "移出分组"
			case "legacy":
				_, err = p.database.Exec("UPDATE canvas_entry_bindings SET creation_config_json=NULL,creation_groups_json=NULL WHERE inbound_id=?", entryID)
				want = "未记录"
			}
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt})
			if err != nil {
				t.Fatal(err)
			}
			if plan.OK || !strings.Contains(strings.Join(plan.Entries[0].Changes, ";"), want) {
				t.Fatalf("expected %s: %+v", want, plan.Entries)
			}
			plan, err = p.service.PlanRemoveCanvasScheme(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true})
			if err != nil || !plan.OK {
				t.Fatalf("confirmed removal: %v %+v", err, plan)
			}
		})
	}
}
