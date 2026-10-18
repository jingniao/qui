package agentprocess

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestRecoveryHandshakeRequiresExplicitParentGrant(t *testing.T) {
	request := Request{Kind: "agent-start-v1", Nonce: strings.Repeat("a", 64), NodeID: "node", Revision: 1, Version: "1.0", AgentUpdates: true}
	encode := func(r Request) string {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(b) + "\n"
	}
	old, _, err := ReadRequest(strings.NewReader(encode(request)))
	if err != nil || old.AgentRecovery {
		t.Fatal("older parent implicitly grants recovery", err)
	}
	request.AgentRecovery = true
	granted, _, err := ReadRequest(strings.NewReader(encode(request)))
	if err != nil || !granted.AgentRecovery {
		t.Fatal("explicit grant lost", err)
	}
	request.AgentUpdates = false
	if _, _, err := ReadRequest(strings.NewReader(encode(request))); err == nil {
		t.Fatal("recovery without update support accepted")
	}
}

func TestControlOnlyHandshakeCannotClaimHealthyProxy(t *testing.T) {
	r := Request{Kind: "agent-start-v1", Nonce: strings.Repeat("b", 64), NodeID: "node", Revision: 1, Version: "test", AgentUpdates: true, AgentRecovery: true, AllowDegraded: true, ControlOnly: true}
	if r.Validate() != nil {
		t.Fatal("valid control-only handshake rejected")
	}
	if ConfirmHealth(r, r.NodeID, r.Revision, r.Version, true, io.Discard) == nil {
		t.Fatal("control-only worker claimed healthy proxy")
	}
	if err := ConfirmHealth(r, r.NodeID, r.Revision, r.Version, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"no-degraded", "no-recovery", "no-updates"} {
		bad := r
		switch mode {
		case "no-degraded":
			bad.AllowDegraded = false
		case "no-recovery":
			bad.AgentRecovery = false
		case "no-updates":
			bad.AgentUpdates = false
		}
		if bad.Validate() == nil {
			t.Fatal("invalid control-only grant accepted", mode)
		}
	}
}
