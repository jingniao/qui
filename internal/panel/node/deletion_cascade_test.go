package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type deletionCascadeProvider func(context.Context, string) (DeletionCascade, error)

func (f deletionCascadeProvider) PrepareNodeDeletion(ctx context.Context, id string) (DeletionCascade, error) {
	return f(ctx, id)
}

func TestNodeDeletionCascadeSharesTransactionAndReturnsPending(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "删除服务器")
	insert(t, r, ctx, "INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('i1','入口',?,'socks5','127.0.0.1',1080,1,1,0,'direct','[]',x'00','x')", n.ID)
	insert(t, r, ctx, "INSERT INTO relay_cleanup_nodes(node_id,reason,next_attempt_at,created_at,updated_at) VALUES(?,'relay cleanup','x','x','x')", n.ID)
	dispatched := false
	r.RelayDeletion = deletionCascadeProvider(func(context.Context, string) (DeletionCascade, error) {
		return DeletionCascade{
			Preview: DeletionImpact{Schemes: []DeletionItem{{ID: "s", Name: "方案"}}, Tunnels: []DeletionItem{}, Rules: []DeletionItem{}, Inbounds: []DeletionInbound{{ID: "i1", Name: "入口", NodeName: n.Name}}, Blockers: []Blocker{}},
			Removed: map[string]bool{"inbound:i1": true},
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "DELETE FROM inbounds WHERE id='i1'")
				return err
			},
			AfterCommit: func(ctx context.Context) ([]CleanupPendingNode, error) {
				current, err := r.Get(ctx, n.ID)
				if err != nil || current.DeletedAt == "" {
					t.Fatalf("cleanup dispatched before commit: %+v %v", current, err)
				}
				dispatched = true
				return []CleanupPendingNode{{NodeID: "other", Name: "离线服务器", Reason: "relay cleanup"}}, nil
			},
		}, nil
	})
	preview, err := r.Preview(ctx, n.ID)
	if err != nil || len(preview.Blockers) != 0 || len(preview.Inbounds) != 1 || len(preview.Schemes) != 1 || preview.Mode != "delete" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	result, err := r.DeleteWithResult(ctx, n.ID, nil, nil, "owner", "ip")
	if err != nil || !dispatched || len(result.PendingNodes) != 1 || result.PendingNodes[0].NodeID != "other" {
		t.Fatalf("deletion: %+v %v dispatched=%v", result, err, dispatched)
	}
	var pending int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM relay_cleanup_nodes WHERE node_id=?", n.ID).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("deleted server cleanup survived: %d %v", pending, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"pendingNodes":[{"nodeId":"other","name":"离线服务器","reason":"relay cleanup"}]`) {
		t.Fatalf("result contract: %s %v", encoded, err)
	}
}

func TestNodeDeletionCascadeRollsBackOnManualBlocker(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "保留服务器")
	insert(t, r, ctx, "INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('i1','入口',?,'socks5','127.0.0.1',1080,1,1,0,'direct','[]',x'00','x')", n.ID)
	insert(t, r, ctx, "INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,credential_id,encrypted_settings,support_tcp,support_udp,failure_policy,backup_outbound_id,created_at) VALUES('o1','手工出站',?,'direct','',0,1,'',x'00',1,1,'Block','','x')", n.ID)
	r.RelayDeletion = deletionCascadeProvider(func(context.Context, string) (DeletionCascade, error) {
		return DeletionCascade{Removed: map[string]bool{"inbound:i1": true}, Preview: DeletionImpact{Blockers: []Blocker{{Kind: "outbound", ID: "other-out", Name: "其他服务器的手工引用"}}}, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM inbounds WHERE id='i1'")
			return err
		}, AfterCommit: func(context.Context) ([]CleanupPendingNode, error) {
			t.Fatal("blocked deletion dispatched cleanup")
			return nil, nil
		}}, nil
	})
	_, err := r.DeleteWithResult(ctx, n.ID, nil, nil, "owner", "ip")
	var blocked *BlockedError
	if !errors.As(err, &blocked) || len(blocked.Items) != 2 || blocked.Items[0].ID != "o1" || blocked.Items[1].ID != "other-out" {
		t.Fatalf("manual blocker missing: %v", err)
	}
	var inbounds int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM inbounds WHERE id='i1'").Scan(&inbounds); err != nil || inbounds != 1 {
		t.Fatalf("cascade deletion escaped rollback: %d %v", inbounds, err)
	}
	current, err := r.Get(ctx, n.ID)
	if err != nil || current.DeletedAt != "" {
		t.Fatalf("blocked deletion changed server: %+v %v", current, err)
	}
}

func TestNodeDeletionEmptyJSONCollections(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "空服务器")
	preview, err := r.Preview(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"schemes", "tunnels", "rules", "inbounds", "blockers"} {
		if !strings.Contains(string(encoded), `"`+field+`":[]`) {
			t.Fatalf("%s must be an empty JSON array: %s", field, encoded)
		}
	}
}
