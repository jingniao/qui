package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestCanvasRemovalRealBatchOrderAndFailureRecovery(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			h := newGostHosts(t)
			for _, nodeID := range []string{h.alpine, h.us2} {
				n, err := h.service.Nodes.Get(h.ctx, nodeID)
				if err != nil {
					t.Fatal(err)
				}
				n.Capabilities.Cores[0].Protocols = append(n.Capabilities.Cores[0].Protocols, &pb.ProtocolCapability{Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true, Udp: true})
				raw, err := json.Marshal(n.Capabilities)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = h.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), nodeID); err != nil {
					t.Fatal(err)
				}
			}
			port := 20000
			layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{
				{InstanceID: "entry", Ref: "server:" + h.alpine, Role: "entry", EntryBinding: &proxy.CanvasEntryBinding{Mode: "create", Protocol: "vless", PortMode: "manual", Port: &port, GroupMode: "none"}},
				{InstanceID: "exit", Ref: "server:" + h.us2, Role: "exit", RelayPortMode: "manual", RelayPort: portValue(30443)},
			}, Connections: []proxy.CanvasConnection{{ID: "hop", Source: "entry", Target: "exit", Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true}}}
			scheme, err := h.service.SaveCanvasScheme(h.ctx, "", proxy.CanvasSchemeInput{Name: "remove", Execution: "server", Layout: layout})
			if err != nil {
				t.Fatal(err)
			}
			h.publishScheme(scheme)
			binding, err := h.service.Repository.CanvasEntryBinding(h.ctx, scheme.ID, "entry")
			if err != nil {
				t.Fatal(err)
			}
			previous := h.revision(h.alpine)
			ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
			defer cancel()
			type response struct {
				result proxy.CanvasRemoveResult
				err    error
			}
			finished := make(chan response, 1)
			go func() {
				result, err := h.service.RemoveCanvasScheme(ctx, scheme.ID, proxy.CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", h.users)
				finished <- response{result, err}
			}()
			order := []string{}
			for {
				select {
				case answer := <-finished:
					var tunnelCount int
					if err := h.database.QueryRow("SELECT count(*) FROM canvas_tunnels WHERE id=?", scheme.Layout.Connections[0].TunnelID).Scan(&tunnelCount); err != nil {
						t.Fatal(err)
					}
					wantCount := 0
					if fail {
						wantCount = 1
					}
					if tunnelCount != wantCount {
						t.Fatalf("tunnel rows %d want %d", tunnelCount, wantCount)
					}
					for _, publication := range answer.result.Publications {
						for _, n := range publication.Nodes {
							node, err := h.service.Nodes.Get(h.ctx, n.NodeID)
							if err != nil || n.Name != node.Name {
								t.Fatalf("publication name %q: %v", n.Name, err)
							}
						}
					}
					if fail {
						if answer.err == nil || !strings.Contains(answer.err.Error(), "controlled_remove_failure") {
							t.Fatalf("error %v", answer.err)
						}
						if _, err = h.service.Repository.CanvasScheme(h.ctx, scheme.ID); err != nil {
							t.Fatal(err)
						}
						entry, err := h.service.Repository.Inbound(h.ctx, binding.InboundID, false)
						if err != nil || !entry.Enabled {
							t.Fatalf("entry %+v: %v", entry, err)
						}
						if h.revision(h.alpine) != previous {
							t.Fatal("entry revision not restored")
						}
						want := []string{h.alpine, h.alpine, h.us2, "rollback:" + h.alpine, "rollback:" + h.alpine}
						if !reflect.DeepEqual(order, want) {
							t.Fatalf("order %v want %v", order, want)
						}
					} else {
						if answer.err != nil || !answer.result.Removed {
							t.Fatalf("result %+v: %v", answer.result, answer.err)
						}
						if _, err = h.service.Repository.CanvasScheme(h.ctx, scheme.ID); !errors.Is(err, proxy.ErrNotFound) {
							t.Fatalf("scheme retained: %v", err)
						}
						want := []string{h.alpine, h.alpine, h.us2}
						if !reflect.DeepEqual(order, want) {
							t.Fatalf("order %v want %v", order, want)
						}
					}
					return
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
					for _, nodeID := range []string{h.alpine, h.us2} {
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
							failed := fail && nodeID == h.us2
							code := ""
							if failed {
								code = "controlled_remove_failure"
							}
							if err = h.repository.Result(ctx, nodeID, &pb.ApplyResult{Revision: state.Revision, Success: !failed, RolledBack: failed, Code: code}); err != nil {
								t.Fatal(err)
							}
						}
						command, err := h.repository.PendingRollback(ctx, nodeID)
						if err != nil {
							t.Fatal(err)
						}
						if command != nil {
							order = append(order, "rollback:"+nodeID)
							if err = h.repository.CommandResult(ctx, nodeID, &pb.ApplyResult{CommandId: command.CommandId, Revision: command.Revision, Success: true}); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
		})
	}
}
