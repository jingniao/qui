package policy

import (
	"context"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
)

// A deleted node can neither join a policy scope nor be simulated.
func TestDeletedNodeRejectedByPolicyScope(t *testing.T) {
	service, nodeID, _, _, _ := fixture(t)
	ctx := context.Background()
	if _, err := service.Repository.Database.ExecContext(ctx, "UPDATE nodes SET deleted_at='2026-01-01T00:00:00.000000000Z' WHERE id=?", nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, "", Input{Name: "Deleted scope", Priority: 10, Enabled: true, Scope: desiredstate.Scope{NodeIDs: []string{nodeID}}, Match: desiredstate.Match{Domains: []string{"example.com"}}, Action: desiredstate.Action{Type: "Block"}}); err == nil {
		t.Fatal("policy accepted a deleted node")
	}
	if _, err := service.Simulate(ctx, shared.Request{NodeID: nodeID, Domain: "example.com", Network: "tcp", Port: 443}); err == nil {
		t.Fatal("simulation accepted a deleted node")
	}
}
