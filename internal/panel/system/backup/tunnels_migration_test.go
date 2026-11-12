package backup

import (
	"context"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/testutil/canvasfixture"
)

func TestCanvasForwardMigrationArchiveVersions(t *testing.T) {
	t.Parallel()
	for _, old := range []bool{true, false} {
		t.Run(map[bool]string{true: "pre0129", false: "v14"}[old], func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			ctx := context.Background()
			seeded := canvasfixture.PublishedForwards(t, source.Database, source.ConfigurationVault)
			if old {
				rewindTunnelFixture(t, source.Database)
			} else {
				if err := (&proxy.Repository{Database: source.Database, Vault: source.ConfigurationVault}).ReconcileCanvasForwards(ctx); err != nil {
					t.Fatal(err)
				}
			}
			created, err := source.Create(ctx, directory, "m1_archive_fixture", "owner", testPassword)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
				database, e := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
				if e != nil {
					return e
				}
				defer database.Close()
				inventory, e := ReadInventory(ctx, database)
				if e != nil {
					return e
				}
				if old {
					if inventory.Totals.CanvasForwardReconcile != 0 || inventory.Totals.CanvasTunnels != 2 || inventory.Totals.CanvasTunnelObjects != 3 {
						t.Fatal(inventory.Totals)
					}
				} else {
					if inventory.Totals.CanvasForwardReconcile != 1 || inventory.Totals.TunnelRules != 2 || inventory.Totals.PortClaims != 3 {
						t.Fatal(inventory.Totals)
					}
					canvasfixture.AssertConverted(t, database, source.ConfigurationVault, seeded, false)
				}
				return proxy.ValidateTunnels(ctx, database, source.ConfigurationVault)
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
