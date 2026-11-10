package deployment

import (
	"context"
	"errors"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

func TestKeywordRejectedForOldAgent(t *testing.T) {
	r, _, _, rev := coreNodeFixture(t, "linux/amd64", "xray")
	ctx := context.Background()
	state, err := r.State(ctx, rev.ID)
	if err != nil {
		t.Fatal(err)
	}

	state.Policies = []desiredstate.Policy{
		{
			ID:       "pol-kw",
			Name:     "Keyword Policy",
			Priority: 10,
			Enabled:  true,
			Action:   desiredstate.Action{Type: "Direct"},
			Match:    desiredstate.Match{Keywords: []string{"openai"}},
		},
	}

	draft, err := r.Create(ctx, state, "owner", "keyword policy")
	if err != nil {
		t.Fatalf("unexpected error creating draft: %v", err)
	}

	err = r.Deploy(ctx, draft.ID)
	if err == nil {
		t.Fatal("expected deploy to fail for old agent without keyword capability")
	}
	if !errors.Is(err, desiredstate.ErrKeywordUnsupported) {
		t.Fatalf("expected ErrKeywordUnsupported, got: %v", err)
	}
}
