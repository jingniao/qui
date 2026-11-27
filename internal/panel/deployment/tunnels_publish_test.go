package deployment

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func managedTunnelFixture(t *testing.T, middle bool) (*gostHosts, proxy.Tunnel, proxy.TunnelRule) {
	t.Helper()
	h := newGostHosts(t)
	for _, nodeID := range []string{h.alpine, h.uk, h.us2} {
		n, err := h.service.Nodes.Get(h.ctx, nodeID)
		if err != nil {
			t.Fatal(err)
		}
		n.Capabilities.Cores[0].ForwardInboundSupported = true
		n.Capabilities.Cores[1].GostContractVersion = 4
		raw, _ := json.Marshal(n.Capabilities)
		if _, err = h.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), nodeID); err != nil {
			t.Fatal(err)
		}
	}
	hops := []proxy.TunnelHop{{Role: "entry", NodeID: h.alpine, PortMode: "auto"}}
	if middle {
		hops = append(hops, proxy.TunnelHop{Role: "mid", NodeID: h.uk, PortMode: "manual", ListenPort: portValue(30010)})
	}
	hops = append(hops, proxy.TunnelHop{Role: "exit", NodeID: h.us2, PortMode: "manual", ListenPort: portValue(30020)})
	tunnel, err := h.service.SaveTunnel(h.ctx, "", proxy.TunnelInput{Name: "HK → JP", Transport: "tls", Hops: hops})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := h.service.SaveTunnelRule(h.ctx, "", proxy.TunnelRuleInput{Name: "HTTPS", EntryNodeID: h.alpine, PortMode: "manual", ListenPort: portValue(30001), Network: "both", TunnelID: &tunnel.ID, Target: proxy.TunnelTarget{Mode: "manual", Host: "203.0.113.5", Port: 443}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return h, tunnel, rule
}

// Drive the actual deployment repository as Agents do, including rollback
// receipts. Tests assert the queue order rather than simulating batch statuses.
func driveTunnelPublish(t *testing.T, h *gostHosts, selection proxy.PublishSelection, failNode string, failedRollback ...string) (proxy.PublishResult, []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()
	type response struct {
		result proxy.PublishResult
		err    error
	}
	finished := make(chan response, 1)
	go func() {
		result, err := h.service.PublishTunnels(ctx, selection, "owner", h.users)
		finished <- response{result, err}
	}()
	order := []string{}
	for {
		select {
		case res := <-finished:
			if res.err != nil {
				t.Fatal(res.err)
			}
			return res.result, order
		case <-ctx.Done():
			t.Fatal("publication timed out")
			return proxy.PublishResult{}, nil
		case <-time.After(10 * time.Millisecond):
			for _, nodeID := range []string{h.alpine, h.uk, h.us2} {
				pending, err := h.repository.Pending(ctx, nodeID)
				if err != nil {
					t.Fatal(err)
				}
				if pending != nil {
					var state desiredstate.State
					if err = json.Unmarshal(pending.DesiredStateJson, &state); err != nil {
						t.Fatal(err)
					}
					order = append(order, nodeID)
					if err = h.repository.Result(ctx, nodeID, &pb.ApplyResult{Revision: state.Revision, Success: nodeID != failNode, RolledBack: nodeID == failNode, Code: "apply_failed"}); err != nil {
						t.Fatal(err)
					}
				}
				command, err := h.repository.PendingRollback(ctx, nodeID)
				if err != nil {
					t.Fatal(err)
				}
				if command != nil {
					order = append(order, "rollback:"+nodeID)
					result := &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}
					if len(failedRollback) > 0 && nodeID == failedRollback[0] {
						result.Success, result.Code = false, "rollback_failed"
					}
					if err = h.repository.CommandResult(ctx, nodeID, result); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}

func TestTunnelPublishPreservesRollbackFailure(t *testing.T) {
	h, tunnel, _ := managedTunnelFixture(t, true)
	selection := proxy.PublishSelection{TunnelIDs: []string{tunnel.ID}}
	driveTunnelPublish(t, h, selection, "")
	stored, err := h.service.Repository.Tunnel(h.ctx, tunnel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.RotateTunnelCredentials(h.ctx, stored.ID, stored.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	result, _ := driveTunnelPublish(t, h, selection, h.alpine, h.uk)
	if result.Status != "rollback_failed" {
		t.Errorf("batch rollback outcome lost: %+v", result)
	}
	for _, item := range result.Servers {
		if item.NodeID == h.uk && item.Status != "rollback_failed" {
			t.Errorf("node rollback outcome lost: %+v", item)
		}
	}
	if err := h.repository.RollbackBatch(h.ctx, result.BatchID); err != nil {
		t.Fatalf("existing batch rollback must accept the failed batch: %v", err)
	}
}

func TestLinkedTunnelPublicationMovesRelayOnlyCredentials(t *testing.T) {
	h := newGostHosts(t)
	createTarget := func(nodeID string) proxy.Inbound {
		t.Helper()
		inbound, err := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{
			Name: "Relay destination", NodeID: nodeID, Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 30801,
			Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{Method: "chacha20-ietf-poly1305"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return inbound
	}
	oldTarget, newTarget := createTarget(h.uk), createTarget(h.us2)
	rule, err := h.service.SaveTunnelRule(h.ctx, "", proxy.TunnelRuleInput{
		Name: "Linked relay", EntryNodeID: h.alpine, ListenPort: portValue(30701), PortMode: "manual", Network: "both",
		Target: proxy.TunnelTarget{Mode: "linked", InboundID: oldTarget.ID}, Enabled: true, SubscriptionVisible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	group, err := (&node.Repository{Database: h.database}).SaveGroup(h.ctx, "", node.GroupInput{
		Name: "Relay only", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, RelayIDs: []string{rule.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.users.Save(h.ctx, "", user.Input{Username: "relay-user", DisplayName: "Relay User", Status: "active", NodeGroupIDs: []string{group.ID}}); err != nil {
		t.Fatal(err)
	}
	assertAccess := func(state desiredstate.State, target proxy.Inbound, want bool) {
		t.Helper()
		for _, inbound := range state.Inbounds {
			if inbound.ID == target.ID {
				if got := inbound.Enabled && len(inbound.CredentialIDs) == 1; got != want {
					t.Errorf("%s: has credentials = %t, want %t", target.NodeID, got, want)
				}
				return
			}
		}
		t.Fatalf("target %s is missing from the state", target.ID)
	}
	assertPublished := func(target proxy.Inbound, want bool) {
		t.Helper()
		state, err := h.repository.Published(h.ctx, target.NodeID)
		if err != nil {
			t.Fatal(err)
		}
		assertAccess(state, target, want)
	}
	// Establish a previously deployed configuration on both destination servers.
	for _, target := range []proxy.Inbound{oldTarget, newTarget} {
		state, intent, err := h.service.SnapshotIntent(h.ctx, target.NodeID, h.users)
		if err != nil {
			t.Fatal(err)
		}
		h.publish("target baseline", []proxy.NodeSnapshot{{State: state, InboundIntent: intent}})
	}
	selection := proxy.PublishSelection{RuleIDs: []string{rule.ID}}
	if result, _ := driveTunnelPublish(t, h, selection, ""); result.Status != "success" {
		t.Fatal(result)
	}
	assertPublished(oldTarget, true)
	input := rule.TunnelRuleInput
	input.Target.InboundID = newTarget.ID
	if _, err := h.service.SaveTunnelRule(h.ctx, rule.ID, input); err != nil {
		t.Fatal(err)
	}
	for _, target := range []proxy.Inbound{oldTarget, newTarget} {
		state, err := h.service.Snapshot(h.ctx, target.NodeID, h.users)
		if err != nil {
			t.Fatal(err)
		}
		assertAccess(state, target, target.ID == oldTarget.ID)
	}
	failed, order := driveTunnelPublish(t, h, selection, h.alpine)
	if failed.Status != "failed" || !slices.Contains(order, "rollback:"+h.us2) {
		t.Errorf("new destination should be rolled back when the entry fails: %+v, %v", failed, order)
	}
	assertPublished(oldTarget, true)
	assertPublished(newTarget, false)
	result, order := driveTunnelPublish(t, h, selection, "")
	if result.Status != "success" || !slices.Equal(order, []string{h.us2, h.alpine, h.uk}) {
		t.Errorf("expected new target, entry, old target in one batch: %+v, %v", result, order)
	}
	assertPublished(oldTarget, false)
	assertPublished(newTarget, true)
}

func TestManagedTunnelPlanPublishNoopRotationAndRemoval(t *testing.T) {
	for _, middle := range []bool{false, true} {
		t.Run(map[bool]string{false: "two", true: "three"}[middle], func(t *testing.T) {
			h, tunnel, rule := managedTunnelFixture(t, middle)
			selection := proxy.PublishSelection{RuleIDs: []string{rule.ID}}
			var before int
			h.database.QueryRow("SELECT count(*) FROM inbounds").Scan(&before)
			plan, err := h.service.PlanTunnels(h.ctx, selection)
			if err != nil {
				t.Fatal(err)
			}
			expected := []string{h.us2, h.alpine}
			if middle {
				expected = []string{h.us2, h.uk, h.alpine}
			}
			order := []string{}
			for _, server := range plan.Servers {
				order = append(order, server.NodeID)
			}
			if !plan.OK || !reflect.DeepEqual(order, expected) || len(plan.Warnings) != 1 {
				t.Fatal(plan)
			}
			var after int
			h.database.QueryRow("SELECT count(*) FROM inbounds").Scan(&after)
			if before != after {
				t.Fatal("plan wrote objects")
			}
			result, actual := driveTunnelPublish(t, h, selection, "")
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal(actual, expected)
			}
			for _, server := range result.Servers {
				if server.Status != "applied" || server.Revision == nil {
					t.Fatal(result)
				}
			}
			stored, err := h.service.Repository.Tunnel(h.ctx, tunnel.ID)
			if err != nil || stored.Status != "published" || stored.DraftVersion != stored.PublishedVersion {
				t.Fatal(stored, err)
			}
			if err = proxy.ValidateTunnels(h.ctx, h.database, h.service.Repository.Vault); err != nil {
				t.Fatal(err)
			}
			plan, err = h.service.PlanTunnels(h.ctx, selection)
			if err != nil {
				t.Fatal(err)
			}
			for _, server := range plan.Servers {
				if len(server.Changes) != 0 {
					t.Fatal("unchanged plan has changes", plan)
				}
			}
			inbounds := h.inbounds(h.alpine)
			if len(inbounds) != 1 || inbounds[0].ManagedBy == nil || inbounds[0].SubscriptionVisible || len(inbounds[0].Settings.Forward.Hops) != 1 {
				t.Fatal(inbounds)
			}
			if middle {
				mid := h.inbounds(h.uk)[0]
				exit := h.inbounds(h.us2)[0]
				if !mid.Settings.Forward.RelayMode() || len(mid.Settings.Forward.Hops) != 1 || mid.Settings.Forward.Relay.CertificatePEM == exit.Settings.Forward.Relay.CertificatePEM {
					t.Fatal("middle must have distinct listener and dialer material")
				}
				if inbounds[0].Settings.Forward.Hops[0].CACertPEM != mid.Settings.Forward.Relay.ClientCAPEM || mid.Settings.Forward.Hops[0].CACertPEM != exit.Settings.Forward.Relay.ClientCAPEM {
					t.Fatal("relay leg credentials do not match their next listener")
				}
			}
			result, actual = driveTunnelPublish(t, h, selection, "")
			if len(actual) != 0 || result.BatchID != "" {
				t.Fatal("republish created work", result, actual)
			}
			for _, server := range result.Servers {
				if server.Status != "unchanged" || server.Revision != nil {
					t.Fatal(result)
				}
			}
			if _, err = h.service.RotateTunnelCredentials(h.ctx, tunnel.ID, stored.UpdatedAt); err != nil {
				t.Fatal(err)
			}
			_, actual = driveTunnelPublish(t, h, proxy.PublishSelection{TunnelIDs: []string{tunnel.ID}}, "")
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal("rotation order", actual)
			}
			if err = h.service.DeleteTunnelRule(h.ctx, rule.ID); err != nil {
				t.Fatal(err)
			}
			driveTunnelPublish(t, h, selection, "")
			if _, err = h.service.Repository.TunnelRule(h.ctx, rule.ID); err == nil {
				t.Fatal("pending rule retained")
			}
			if err = h.service.DeleteTunnel(h.ctx, tunnel.ID); err != nil {
				t.Fatal(err)
			}
			driveTunnelPublish(t, h, proxy.PublishSelection{TunnelIDs: []string{tunnel.ID}}, "")
			for _, table := range []string{"tunnels", "tunnel_rules", "tunnel_objects", "port_claims", "inbounds"} {
				var count int
				if err = h.database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatal(table, count, err)
				}
			}
		})
	}
}

func TestTunnelPublishGostRemovable(t *testing.T) {
	for _, mode := range []string{"unused", "manual", "rule", "missing"} {
		t.Run(mode, func(t *testing.T) {
			h, tunnel, rule := managedTunnelFixture(t, false)
			driveTunnelPublish(t, h, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "")
			if mode == "manual" {
				_, err := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{Name: "manual", NodeID: h.alpine, Core: "gost", Protocol: "forward", Listen: "0.0.0.0", Port: 31000, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "rule" {
				_, err := h.service.SaveTunnelRule(h.ctx, "", proxy.TunnelRuleInput{Name: "other", EntryNodeID: h.alpine, PortMode: "manual", ListenPort: portValue(31000), Network: "tcp", Target: proxy.TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
				// Publish the independent rule so its GOST inbound remains.
				var rid string
				if err := h.database.QueryRow("SELECT id FROM tunnel_rules WHERE name='other'").Scan(&rid); err != nil {
					t.Fatal(err)
				}
				driveTunnelPublish(t, h, proxy.PublishSelection{RuleIDs: []string{rid}}, "")
			}
			if err := h.service.DeleteTunnelRule(h.ctx, rule.ID); err != nil {
				t.Fatal(err)
			}
			driveTunnelPublish(t, h, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "")
			if mode == "missing" {
				n, err := h.service.Nodes.Get(h.ctx, h.alpine)
				if err != nil {
					t.Fatal(err)
				}
				n.Capabilities.Cores = n.Capabilities.Cores[:1]
				raw, err := json.Marshal(n.Capabilities)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = h.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), h.alpine); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.service.DeleteTunnel(h.ctx, tunnel.ID); err != nil {
				t.Fatal(err)
			}
			result, _ := driveTunnelPublish(t, h, proxy.PublishSelection{TunnelIDs: []string{tunnel.ID}}, "")
			got := map[string]bool{}
			for _, item := range result.GostRemovable {
				got[item.NodeID] = true
				if item.Name == "" {
					t.Fatal(item)
				}
			}
			if !got[h.us2] || got[h.alpine] != (mode == "unused") || len(got) != map[bool]int{true: 2, false: 1}[mode == "unused"] {
				t.Fatal(result)
			}
		})
	}
}

func TestManagedRulePortClaimsReleaseOnlyAfterSuccessfulApply(t *testing.T) {
	h, tunnel, rule := managedTunnelFixture(t, false)
	selection := proxy.PublishSelection{RuleIDs: []string{rule.ID}}
	driveTunnelPublish(t, h, selection, "")
	rule, err := h.service.Repository.TunnelRule(h.ctx, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := rule.TunnelRuleInput
	input.ListenPort = portValue(30002)
	if _, err = h.service.SaveTunnelRule(h.ctx, rule.ID, input); err != nil {
		t.Fatal(err)
	}
	result, order := driveTunnelPublish(t, h, selection, h.alpine)
	if len(order) != 1 || order[0] != h.alpine || result.Servers[len(result.Servers)-1].Status != "rolled_back" {
		t.Fatal(result, order)
	}
	if got := result.Servers[len(result.Servers)-1].Message; got != "配置应用失败，已自动回滚（apply_failed）" {
		t.Fatal(got)
	}
	for _, port := range []int{30001, 30002} {
		c, e := h.service.Repository.CheckPort(h.ctx, h.alpine, port, nil)
		if e != nil || c.Free {
			t.Fatal(c, e)
		}
	}
	if err = proxy.ValidateTunnels(h.ctx, h.database, h.service.Repository.Vault); err != nil {
		t.Fatal(err)
	}
	result, order = driveTunnelPublish(t, h, selection, "")
	if len(order) != 1 || order[0] != h.alpine {
		t.Fatal(result, order)
	}
	old, err := h.service.Repository.CheckPort(h.ctx, h.alpine, 30001, nil)
	if err != nil || !old.Free {
		t.Fatal(old, err)
	}
	stored, _ := h.service.Repository.Tunnel(h.ctx, tunnel.ID)
	if stored.Status != "published" {
		t.Fatal(stored)
	}
}

func TestManagedTunnelFailureRollsBackInReverseAndKeepsClaims(t *testing.T) {
	h, tunnel, rule := managedTunnelFixture(t, true)
	selection := proxy.PublishSelection{TunnelIDs: []string{tunnel.ID}}
	driveTunnelPublish(t, h, selection, "")
	stored, _ := h.service.Repository.Tunnel(h.ctx, tunnel.ID)
	if _, err := h.service.RotateTunnelCredentials(h.ctx, tunnel.ID, stored.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	result, order := driveTunnelPublish(t, h, selection, h.alpine)
	expected := []string{h.us2, h.uk, h.alpine, "rollback:" + h.uk, "rollback:" + h.us2}
	if !reflect.DeepEqual(order, expected) {
		t.Fatal(order, expected)
	}
	if result.Servers[0].Status != "rolled_back" || result.Servers[1].Status != "rolled_back" || result.Servers[2].Status != "rolled_back" {
		t.Fatal(result)
	}
	for i, want := range []string{"后续服务器失败，已回滚", "后续服务器失败，已回滚", "配置应用失败，已自动回滚（apply_failed）"} {
		if result.Servers[i].Message != want {
			t.Fatal(result.Servers)
		}
	}
	stored, _ = h.service.Repository.Tunnel(h.ctx, tunnel.ID)
	if stored.Status != "modified" || stored.PublishedVersion == stored.DraftVersion {
		t.Fatal(stored)
	}
	if err := proxy.ValidateTunnels(h.ctx, h.database, h.service.Repository.Vault); err != nil {
		t.Fatal(err)
	}
	check, err := h.service.Repository.CheckPort(h.ctx, h.alpine, *rule.ListenPort, nil)
	if err != nil || check.Free {
		t.Fatal(check, err)
	}
}

func TestManagedDirectForwardCoreSelection(t *testing.T) {
	for _, core := range []string{"gost", "xray", "sing-box"} {
		t.Run(core, func(t *testing.T) {
			h := newGostHosts(t)
			caps := &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, ForwardInboundSupported: true, Cores: []*pb.CoreCapability{{Name: "xray", ForwardInboundSupported: true}}}
			if core == "sing-box" {
				caps.CoreName = core
				caps.Cores[0].Name = core
			}
			if core == "gost" {
				caps.Cores = append(caps.Cores, &pb.CoreCapability{Name: "gost", ForwardInboundSupported: true, GostContractVersion: 4})
			}
			raw, _ := json.Marshal(caps)
			h.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), h.alpine)
			rule, err := h.service.SaveTunnelRule(h.ctx, "", proxy.TunnelRuleInput{Name: "direct", EntryNodeID: h.alpine, PortMode: "manual", ListenPort: portValue(30100), Network: "both", Target: proxy.TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			result, _ := driveTunnelPublish(t, h, proxy.PublishSelection{RuleIDs: []string{rule.ID}}, "")
			if result.Servers[0].Status != "applied" {
				t.Fatal(result)
			}
			in := h.inbounds(h.alpine)[0]
			if in.Core != core || len(in.Settings.Forward.Hops) != 0 {
				t.Fatal(in)
			}
			state, err := h.repository.Published(h.ctx, h.alpine)
			if err != nil || state.Validate() != nil {
				t.Fatal(state, err)
			}
		})
	}
}
