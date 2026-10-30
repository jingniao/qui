package identity

import (
	"testing"
	"time"

	"proxycontrol/internal/platform/pki"
)

func TestRenewalAtThirtyDaysOrFourteenRemaining(t *testing.T) {
	dir := t.TempDir()
	authority, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := CSR(dir)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(raw)
	if err != nil {
		t.Fatal(err)
	}
	cert, leaf, err := authority.IssueNode(csr, "renew-policy")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveEnrollment(dir, "renew-policy", cert, authority.PEM); err != nil {
		t.Fatal(err)
	}
	state, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		at  time.Time
		due bool
	}{{leaf.NotBefore.Add(30*24*time.Hour - time.Second), false}, {leaf.NotBefore.Add(30 * 24 * time.Hour), true}, {leaf.NotBefore.Add(31 * 24 * time.Hour), true}, {leaf.NotAfter.Add(-14 * 24 * time.Hour), true}} {
		due, err := state.RenewalDue(tt.at)
		if err != nil || due != tt.due {
			t.Fatalf("%v: due=%v want=%v err=%v", tt.at, due, tt.due, err)
		}
	}
}
