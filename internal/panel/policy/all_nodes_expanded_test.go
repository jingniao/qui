package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

// bigServiceSet saves two sets of 1500 domains (one set is limited to 2000
// entries); a policy referencing both expands to 3000.
func bigServiceSet(t *testing.T, service *Service) []string {
	t.Helper()
	ids := []string{}
	for n := 0; n < 2; n++ {
		domains := make([]string, 0, 1500)
		for i := 0; i < 1500; i++ {
			domains = append(domains, fmt.Sprintf("host%d-%d.example.test", n, i))
		}
		set, err := service.Repository.SaveService(context.Background(), "", ServiceInput{Name: fmt.Sprintf("big set %d", n), Enabled: true, Domains: domains})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, set.ID)
	}
	return ids
}

func allNodesPolicy(setIDs []string, enabled bool) Input {
	return Input{Name: "all nodes big", Priority: 10, Enabled: enabled, Match: desiredstate.Match{ServiceIDs: setIDs}, Action: desiredstate.Action{Type: "Block"}}
}

func TestAllNodesPolicyListsUnsupportedServersAtSave(t *testing.T) {
	service, nodeID, _, _, _ := fixture(t)
	ctx := context.Background()
	setID := bigServiceSet(t, service)
	nodes := &node.Repository{Database: service.Repository.Database}
	old, _, _, err := nodes.CreateEnrollment(ctx, "Old agent node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	supported := map[string]bool{nodeID: true, old.ID: false}
	service.NodeCapabilities = func(_ context.Context, id string) ([]desiredstate.CoreKeywordCapability, error) {
		return []desiredstate.CoreKeywordCapability{{Name: desiredstate.CoreXray, ExpandedPolicySupported: supported[id]}}, nil
	}
	_, err = service.Save(ctx, "", allNodesPolicy(setID, true))
	if !errors.Is(err, desiredstate.ErrExpandedPolicyUnsupported) {
		t.Fatalf("expected expanded policy error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Old agent node") || strings.Contains(err.Error(), "Policy node") {
		t.Fatalf("error should list only the unsupported server: %v", err)
	}
	// A disabled policy is not checked, as with an explicit scope.
	if _, err := service.Save(ctx, "", allNodesPolicy(setID, false)); err != nil {
		t.Fatalf("disabled policy rejected: %v", err)
	}
}

func TestAllNodesPolicyAcceptedWhenAllSupported(t *testing.T) {
	service, _, _, _, _ := fixture(t)
	ctx := context.Background()
	setID := bigServiceSet(t, service)
	service.NodeCapabilities = func(context.Context, string) ([]desiredstate.CoreKeywordCapability, error) {
		return []desiredstate.CoreKeywordCapability{{Name: desiredstate.CoreXray, ExpandedPolicySupported: true}}, nil
	}
	if _, err := service.Save(ctx, "", allNodesPolicy(setID, true)); err != nil {
		t.Fatalf("all supported rejected: %v", err)
	}
}

func TestAllNodesPolicyIgnoresDeletedNodes(t *testing.T) {
	service, _, _, _, _ := fixture(t)
	ctx := context.Background()
	setID := bigServiceSet(t, service)
	nodes := &node.Repository{Database: service.Repository.Database}
	old, _, _, err := nodes.CreateEnrollment(ctx, "Deleted old node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Repository.Database.ExecContext(ctx, "UPDATE nodes SET deleted_at='2026-01-01T00:00:00.000000000Z' WHERE id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	service.NodeCapabilities = func(_ context.Context, id string) ([]desiredstate.CoreKeywordCapability, error) {
		return []desiredstate.CoreKeywordCapability{{Name: desiredstate.CoreXray, ExpandedPolicySupported: id != old.ID}}, nil
	}
	if _, err := service.Save(ctx, "", allNodesPolicy(setID, true)); err != nil {
		t.Fatalf("deleted node should be ignored: %v", err)
	}
}
