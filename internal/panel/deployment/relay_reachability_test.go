package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestRelayDependencyOnlyReachableOutbounds(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	captured := relaySourceAndTarget(states)
	stale := states[1]
	stale.Inbounds = append([]desiredstate.Inbound(nil), stale.Inbounds...)
	stale.Inbounds[0].Port++
	applySnapshot(t, r, stale)
	state := captured[0].State
	state.Inbounds[0].DefaultOutbound = "direct"
	for _, reachable := range []bool{false, true} {
		if reachable {
			state.Inbounds[0].DefaultOutbound = "next"
		}
		tx, err := r.Database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		results := []error{
			r.relayTargetsInTransaction(ctx, tx, state),
			r.validateBatchDependencies(ctx, tx, []proxy.NodeSnapshot{{State: state}, captured[1]}),
		}
		tx.Rollback()
		results = append(results, checkRelayTargets(ctx, r, state))
		for _, err := range results {
			if !reachable && err != nil {
				t.Fatalf("unreachable relay blocked: %v", err)
			}
			if reachable && (!errors.Is(err, ErrDependency) || !strings.Contains(err.Error(), "next") || !strings.Contains(err.Error(), states[1].NodeID) || !strings.Contains(err.Error(), "internal_inbound_unavailable")) {
				t.Fatalf("missing dependency identity: %v", err)
			}
		}
	}
}

func TestPublishAcceptsEmptyShadowsocksMethod(t *testing.T) {
	ctx := context.Background()
	r, states, _ := batchFixture(t)
	state := states[0]
	state.Inbounds[0].Protocol = "shadowsocks"
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := validateStateCoresTx(ctx, tx, state); err != nil {
		t.Fatalf("pre-publish: %v", err)
	}
	state.Inbounds[0].Settings.Method = "chacha20-ietf-poly1305"
	if err := validateStateCoresTx(ctx, tx, state); err != nil {
		t.Fatal(err)
	}
}

func TestShadowsocksHistoricalMethodOperations(t *testing.T) {
	for _, operation := range []string{"restart", "rollback", "deploy", "batch"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", operation, enabled), func(t *testing.T) {
				ctx := context.Background()
				r, states, _ := batchFixture(t)
				state := states[0]
				revision := applySnapshot(t, r, state)
				state.Revision = revision
				state.Inbounds[0].Protocol = "shadowsocks"
				state.Inbounds[0].Enabled = enabled
				state.Inbounds[0].CredentialIDs = []string{"ss-credential"}
				state.Credentials = []desiredstate.Credential{{ID: "ss-credential", Protocol: "shadowsocks", Internal: true, Secret: "test-secret"}}
				// Model a successfully applied historical revision, preserving its original empty method.
				raw, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				sealed, err := r.Vault.Seal(raw)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.Database.Exec("UPDATE config_revisions SET encrypted_state=? WHERE id=?", sealed, revision); err != nil {
					t.Fatal(err)
				}
				if _, err := r.Database.Exec("UPDATE nodes SET core_restart_supported=1,status='online',last_heartbeat=? WHERE id=?", time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"), state.NodeID); err != nil {
					t.Fatal(err)
				}
				switch operation {
				case "restart":
					_, err = r.RestartCore(ctx, state.NodeID, revision, "")
				case "rollback":
					newer := states[0]
					newer.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
					newer.Inbounds[0].Protocol = "socks5"
					newer.Inbounds[0].CredentialIDs = nil
					applySnapshot(t, r, newer)
					_, err = r.RollbackNode(ctx, state.NodeID, revision)
				case "deploy":
					draft, createErr := r.Create(ctx, state, "owner", "legacy method")
					if createErr != nil {
						t.Fatal(createErr)
					}
					err = r.Deploy(ctx, draft.ID)
				case "batch":
					batch, createErr := r.CreateBatch(ctx, "", "legacy method", "owner", batchSnapshots([]desiredstate.State{state, states[1]}))
					if createErr != nil {
						t.Fatal(createErr)
					}
					err = r.StartBatch(ctx, batch.ID)
				}
				if err != nil {
					t.Fatalf("%s: %v", operation, err)
				}
			})
		}
	}
}
