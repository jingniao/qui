package published_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/node"
	panelproxy "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestCanvasEntrySubscriptionNames(t *testing.T) {
	for _, tc := range []struct {
		name, mode, entryName string
		second                string
		want                  []string
	}{
		{"create", "create", "A → B", "", []string{"A → B"}},
		{"multi_hop", "create", "A → M → B", "", []string{"A → M → B"}},
		{"reuse", "reuse", "A → B", "", []string{"US · socks5"}},
		{"ordinary", "", "A → B", "", []string{"US · socks5"}},
		{"collision", "create", "US · socks5", "ordinary", []string{"US · socks5 · 443", "US · socks5 · 1080"}},
		{"created_collision", "create", "A → B", "create", []string{"A → B · 443", "A → B · 1080"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			if _, err := database.Exec(`INSERT INTO nodes(id,name,status,created_at) VALUES('destination','US','online','now')`); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('inbound',?,'destination','socks5','0.0.0.0',443,1,1,0,'direct','[]',x'00','now')`, tc.entryName); err != nil {
				t.Fatal(err)
			}
			if tc.mode != "" {
				if _, err := database.Exec(`INSERT INTO canvas_entry_bindings(id,instance_id,inbound_id,mode,original_default_outbound,state,updated_at) VALUES('binding','entry','inbound',?,'direct','applied','now')`, tc.mode); err != nil {
					t.Fatal(err)
				}
			}
			f := &canvasNameFixture{state: desiredstate.State{NodeID: "destination", Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "identity", Protocol: "socks5", Username: "alice", Secret: "secret"}}, Inbounds: []desiredstate.Inbound{{ID: "inbound", Protocol: "socks5", Listen: "0.0.0.0", Port: 443, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{"credential"}}}}}
			if tc.second != "" {
				inbound := f.state.Inbounds[0]
				inbound.ID, inbound.Port = "ordinary", 1080
				f.state.Inbounds = append(f.state.Inbounds, inbound)
				if tc.second == "create" {
					if _, err := database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('ordinary',?,'destination','socks5','0.0.0.0',1080,1,1,0,'direct','[]',x'00','now')`, tc.entryName); err != nil {
						t.Fatal(err)
					}
					if _, err := database.Exec(`INSERT INTO canvas_entry_bindings(id,instance_id,inbound_id,mode,state,updated_at) VALUES('second-binding','second','ordinary','create','applied','now')`); err != nil {
						t.Fatal(err)
					}
				}
			}
			s := &published.Source{Users: f, Nodes: f, Published: f, CanvasEntries: &panelproxy.Repository{Database: database}}
			endpoints, _, err := s.Proxies(ctx, source.Viewer{UserID: "user", DeviceID: "identity", NodeGroupIDs: []string{"group"}, AllowedProtocols: []string{"socks5"}})
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, endpoint := range endpoints {
				names = append(names, endpoint.Name)
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("names = %q, want %q", names, tc.want)
			}
			// Business metadata must never make an unpublished inbound visible.
			f.state.Inbounds = nil
			endpoints, _, err = s.Proxies(ctx, source.Viewer{UserID: "user", DeviceID: "identity", NodeGroupIDs: []string{"group"}, AllowedProtocols: []string{"socks5"}})
			if err != nil || len(endpoints) != 0 {
				t.Fatalf("unpublished endpoints = %+v, error = %v", endpoints, err)
			}
		})
	}
}

func TestCanvasEntryNameReadErrorFailsSubscription(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	f := &canvasNameFixture{}
	s := &published.Source{Users: f, Nodes: f, Published: f, CanvasEntries: &panelproxy.Repository{Database: database}}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = s.Proxies(canceled, source.Viewer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

type canvasNameFixture struct{ state desiredstate.State }

func (f *canvasNameFixture) Credentials(context.Context, string, bool) ([]user.Credential, error) {
	return []user.Credential{{ID: "credential", Protocol: "socks5", Status: "active", Username: "alice", Secret: "secret"}}, nil
}
func (f *canvasNameFixture) ExpandedNodes(context.Context, []string) ([]node.Node, error) {
	return []node.Node{{ID: "destination", Name: "US", PublicIP: "203.0.113.1", Capabilities: &pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true}}}}}, nil
}
func (f *canvasNameFixture) UserInboundGroups(context.Context, string, string) ([]string, error) {
	return []string{"group"}, nil
}
func (f *canvasNameFixture) Published(context.Context, string) (desiredstate.State, error) {
	return f.state, nil
}
