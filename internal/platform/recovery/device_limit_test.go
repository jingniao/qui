package recovery

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
)

func TestDeviceLimitRestorePreservesConservativeLimit(t *testing.T) {
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		if _, err := f.source.Database.Exec("UPDATE users SET device_limit=7 WHERE id=?", f.user.ID); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec("UPDATE users SET device_limit=2 WHERE id=?", f.user.ID); err != nil {
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
	assertInt(t, restored, "SELECT device_limit FROM users WHERE id=?", 2, f.user.ID)
}
