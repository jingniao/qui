package deployment

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/pkg/contracts/desiredstate"
)

type inboundStateFixture struct {
	repository *Repository
	ctx        context.Context
	nodeID     string
}

func newInboundStateFixture(t *testing.T) *inboundStateFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	n, _, _, err := (&node.Repository{Database: database}).CreateEnrollment(ctx, "State node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &inboundStateFixture{repository: &Repository{Database: database, Vault: key}, ctx: ctx, nodeID: n.ID}
}

func (f *inboundStateFixture) state(inbounds ...desiredstate.Inbound) desiredstate.State {
	credentials := []desiredstate.Credential{}
	for _, inbound := range inbounds {
		for _, id := range inbound.CredentialIDs {
			credentials = append(credentials, desiredstate.Credential{ID: id, Protocol: "http", Username: id, Secret: "secret-" + id})
		}
	}
	return desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: f.nodeID, Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: credentials, Inbounds: inbounds,
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}}
}

func httpInbound(id string, port int, enabled bool, credentials ...string) desiredstate.Inbound {
	return desiredstate.Inbound{ID: id, Protocol: "http", Listen: "127.0.0.1", Port: port, Enabled: enabled, DefaultOutbound: "direct", CredentialIDs: credentials}
}

// revision stores a revision with the given intent and records the node's
// deployment target for it. running makes it the revision the node runs.
func (f *inboundStateFixture) revision(t *testing.T, state desiredstate.State, intent []string, status, code string, running bool) int64 {
	t.Helper()
	revision, err := f.repository.CreateWithInboundIntent(f.ctx, state, intent, "owner", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repository.Database.ExecContext(f.ctx, "INSERT INTO deployment_targets(revision,node_id,status,result_code,updated_at) VALUES(?,?,?,?,'x')", revision.ID, f.nodeID, status, code); err != nil {
		t.Fatal(err)
	}
	if running {
		if _, err := f.repository.Database.ExecContext(f.ctx, "INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET revision=excluded.revision", f.nodeID, revision.ID); err != nil {
			t.Fatal(err)
		}
	}
	return revision.ID
}

func TestInboundStates(t *testing.T) {
	published := httpInbound("web", 8080, true, "web-user")
	neverPublished := httpInbound("hy2", 36122, true, "hy2-user")
	modified := published
	modified.Port = 8081
	otherCredential := published
	otherCredential.CredentialIDs = []string{"other-user"}
	disabled := httpInbound("off", 9000, false)
	noUsers := published
	noUsers.Enabled = false
	noUsers.CredentialIDs = nil

	tests := []struct {
		name  string
		setup func(t *testing.T, f *inboundStateFixture) (draft desiredstate.State, intent []string)
		want  map[string]InboundState
	}{
		{
			name: "new inbound never published and no running revision",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				return f.state(neverPublished), []string{"hy2"}
			},
			want: map[string]InboundState{"hy2": {State: InboundUnpublished}},
		},
		{
			name: "published and running",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(published), []string{"web"}, "success", "", true)
				return f.state(published), []string{"web"}
			},
			want: map[string]InboundState{"web": {State: InboundEffective}},
		},
		{
			name: "published and then modified",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(published), []string{"web"}, "success", "", true)
				return f.state(modified), []string{"web"}
			},
			want: map[string]InboundState{"web": {State: InboundModified}},
		},
		{
			name: "user access changes never count as a modification",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(published), []string{"web"}, "success", "", true)
				return f.state(otherCredential), []string{"web"}
			},
			want: map[string]InboundState{"web": {State: InboundEffective}},
		},
		{
			name: "an inbound without users is still the enabled inbound the Owner published",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(published), []string{"web"}, "success", "", true)
				return f.state(noUsers), []string{"web"}
			},
			want: map[string]InboundState{"web": {State: InboundEffective}},
		},
		{
			name: "enabling a published disabled inbound is a modification",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(disabled), []string{}, "success", "", true)
				return f.state(disabled), []string{"off"}
			},
			want: map[string]InboundState{"off": {State: InboundModified}},
		},
		{
			name: "disabled inbound published disabled is effective",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(disabled), []string{}, "success", "", true)
				return f.state(disabled), []string{}
			},
			want: map[string]InboundState{"off": {State: InboundEffective}},
		},
		{
			name: "failed attempt reports its code",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(published), []string{"web"}, "success", "", true)
				f.revision(t, f.state(published, neverPublished), []string{"web", "hy2"}, "rollback", "apply_failed", false)
				return f.state(published, neverPublished), []string{"web", "hy2"}
			},
			want: map[string]InboundState{"web": {State: InboundEffective}, "hy2": {State: InboundUnpublished, Code: "apply_failed"}},
		},
		{
			name: "automatic quota revision without the inbound keeps the earlier failure",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(published), []string{"web"}, "success", "", false)
				f.revision(t, f.state(published, neverPublished), []string{"web", "hy2"}, "failed", "apply_failed", false)
				f.revision(t, f.state(published), []string{"web"}, "success", "", true)
				return f.state(published, neverPublished), []string{"web", "hy2"}
			},
			want: map[string]InboundState{"web": {State: InboundEffective}, "hy2": {State: InboundUnpublished, Code: "apply_failed"}},
		},
		{
			name: "newer revision waiting for the node is pending",
			setup: func(t *testing.T, f *inboundStateFixture) (desiredstate.State, []string) {
				f.revision(t, f.state(published), []string{"web"}, "success", "", true)
				f.revision(t, f.state(published, neverPublished), []string{"web", "hy2"}, "pending", "", false)
				return f.state(published, neverPublished), []string{"web", "hy2"}
			},
			want: map[string]InboundState{"web": {State: InboundEffective}, "hy2": {State: InboundPending}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newInboundStateFixture(t)
			draft, intent := test.setup(t, f)
			items, err := f.repository.InboundStates(f.ctx, f.nodeID, draft, intent)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != len(test.want) {
				t.Fatalf("got %d states, want %d: %+v", len(items), len(test.want), items)
			}
			for _, item := range items {
				want, ok := test.want[item.InboundID]
				if !ok || item.State != want.State || item.Code != want.Code {
					t.Fatalf("inbound %s: got %+v, want %+v", item.InboundID, item, want)
				}
				if item.Code != "" && item.Revision == 0 {
					t.Fatalf("inbound %s: failure code without revision: %+v", item.InboundID, item)
				}
			}
		})
	}
}
