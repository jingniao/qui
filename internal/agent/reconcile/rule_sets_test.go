package reconcile

import (
	"context"
	"fmt"
	"testing"

	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/pkg/contracts/desiredstate"
)

type ruleSetAdapter struct {
	fakeCoreAdapter
	prepareErr error
	prepared   int
	cleaned    [][]int64
}

func (a *ruleSetAdapter) PrepareRuleSets(context.Context, desiredstate.State) error {
	a.prepared++
	return a.prepareErr
}

func (a *ruleSetAdapter) CleanupRuleSets(states ...desiredstate.State) {
	revisions := []int64{}
	for _, state := range states {
		revisions = append(revisions, state.Revision)
	}
	a.cleaned = append(a.cleaned, revisions)
}

func TestApplyReportsRuleSetUnavailableWithoutRenderingOrCaching(t *testing.T) {
	adapter := &ruleSetAdapter{prepareErr: fmt.Errorf("%w: geosite-cn", rulesets.ErrUnavailable)}
	manager, err := New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Apply(context.Background(), state(freePort(t), 1))
	if err == nil || result.Code != "rule_set_unavailable" || result.Success {
		t.Fatalf("unexpected result: %+v %v", result, err)
	}
	if adapter.renderCalls != 0 || adapter.applyCalls != 0 {
		t.Fatal("a revision with unavailable rule sets was rendered or applied")
	}
	if permanentFailure(result.Code) {
		t.Fatal("rule_set_unavailable must stay retryable")
	}
	// The same revision applies once the files exist.
	adapter.prepareErr = nil
	if result, err := manager.Apply(context.Background(), state(freePort(t), 1)); err != nil || !result.Success {
		t.Fatalf("retry failed: %+v %v", result, err)
	}
}

func TestApplyCleansRuleSetsKeepingTheRetainedRevisions(t *testing.T) {
	adapter := &ruleSetAdapter{}
	manager, err := New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	for revision := int64(1); revision <= 2; revision++ {
		if result, err := manager.Apply(context.Background(), state(freePort(t), revision)); err != nil || !result.Success {
			t.Fatalf("revision %d: %+v %v", revision, result, err)
		}
	}
	last := adapter.cleaned[len(adapter.cleaned)-1]
	if len(last) != 2 || last[0] != 2 || last[1] != 1 {
		t.Fatalf("cleanup must consider the new and the previous revision, got %v", last)
	}
}
