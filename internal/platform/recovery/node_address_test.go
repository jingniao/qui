package recovery

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
)

// The connect address and the manual flag are ordinary node columns, so an
// archive carries them and a restore brings them back. Only a node that exists
// solely in the running database keeps its own values.
func TestNodeConnectAddressSurvivesBackupAndRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		if _, err := f.source.Database.Exec("UPDATE nodes SET public_ip='203.0.113.9',public_ip_manual=1,connect_host='edge.example.com' WHERE id=?", f.node.ID); err != nil {
			t.Fatal(err)
		}
	})
	// The running panel then changes its own values; the archive must win.
	if _, err := f.source.Database.Exec("UPDATE nodes SET public_ip='198.51.100.1',public_ip_manual=0,connect_host='' WHERE id=?", f.node.ID); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertText(t, restored, "SELECT public_ip FROM nodes WHERE id=?", "203.0.113.9", f.node.ID)
	assertInt(t, restored, "SELECT public_ip_manual FROM nodes WHERE id=?", 1, f.node.ID)
	assertText(t, restored, "SELECT connect_host FROM nodes WHERE id=?", "edge.example.com", f.node.ID)
}
