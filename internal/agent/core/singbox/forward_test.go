package singbox

import (
	"context"
	"os"
	"path/filepath"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestForwardGolden(t *testing.T) {
	s := goldenScenarios()[0].state
	s.Credentials = nil
	s.Users = nil
	s.Inbounds[0].Protocol = "forward"
	s.Inbounds[0].CredentialIDs = nil
	s.Inbounds[0].Settings = desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp,udp"}}
	r, err := Render(context.Background(), s, goldenAt)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "forward.json")
	if os.Getenv("UPDATE_FORWARD_GOLDEN") == "1" {
		if err = os.WriteFile(path, r.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(r.Data) {
		t.Fatalf("forward golden mismatch: %s", r.Data)
	}
	if binary := os.Getenv("SINGBOX_BINARY"); binary != "" {
		a := &Adapter{Path: binary, Directory: t.TempDir()}
		if err = a.Validate(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRealForward(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("SINGBOX_BINARY is unset")
	}
	testutil.RealForward(t, binary, []string{"run", "-c"}, func(s desiredstate.State) ([]byte, error) {
		r, e := Render(context.Background(), s, goldenAt)
		return r.Data, e
	})
}
