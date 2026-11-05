package recovery

import (
	"context"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/testutil/canvasfixture"
	"proxycontrol/migrations"
)

func TestCanvasForwardMigrationRestoreVersions(t *testing.T) {
	for _, old := range []bool{true, false} {
		t.Run(map[bool]string{true: "pre0129", false: "v14"}[old], func(t *testing.T) {
			ctx := context.Background()
			var seeded canvasfixture.Fixture
			f := listeningFixture(t, func(f *fixture) {
				seeded = canvasfixture.PublishedForwards(t, f.source.Database, f.source.ConfigurationVault)
				if old {
					rewindTunnelFixture(t, f.source.Database)
				} else {
					if err := (&proxy.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}).ReconcileCanvasForwards(ctx); err != nil {
						t.Fatal(err)
					}
				}
			})
			if err := db.Migrate(ctx, f.source.Database, migrations.Files); err != nil {
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
			canvasfixture.AssertConverted(t, restored, f.source.ConfigurationVault, seeded, true)
		})
	}
}
