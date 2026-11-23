package node

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestLegacyStateResetReplaysOriginalAndRejectsDifferentKey(t *testing.T) {
	p := newRecoveryPanel(t)
	n, old := enrolledAgentNode(t, p)
	c := &Control{Repository: p.Repository, Authority: p.authority}
	csr, _ := signedRequest(t)
	request := &pb.LegacyStateResetRequest{ResetId: "reset-test", CsrPem: csr, TargetSha256: strings.Repeat("b", 64), AgentVersion: "1.10.1"}
	first, err := c.ResetLegacyState(agentPeer(t, old), request)
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}
	if first.NodeId != n.ID {
		t.Fatalf("node changed: %s", first.NodeId)
	}
	if err := p.ValidateIdentity(p.ctx, n.ID, pki.Fingerprint(old)); !errors.Is(err, ErrIdentity) {
		t.Fatalf("old identity survived: %v", err)
	}
	again, err := c.ResetLegacyState(agentPeer(t, old), request)
	if err != nil || !bytes.Equal(first.CertificatePem, again.CertificatePem) {
		t.Fatalf("replay changed: %v", err)
	}
	other, _ := signedRequest(t)
	request.CsrPem = other
	if _, err := c.ResetLegacyState(agentPeer(t, old), request); err == nil {
		t.Fatal("different key reused reset")
	}
	var discarded string
	if err := p.Database.QueryRowContext(p.ctx, "SELECT discarded_usage FROM agent_state_resets WHERE node_id=?", n.ID).Scan(&discarded); err != nil || discarded != "unknown" {
		t.Fatalf("discard status %q: %v", discarded, err)
	}
}
