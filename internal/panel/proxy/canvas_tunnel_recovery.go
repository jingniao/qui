package proxy

import (
	"context"
	"database/sql"
	"errors"

	"proxycontrol/internal/platform/vault"
)

// ValidateCanvasTunnelMaterial checks the sealed GOST credential sets of an
// archive without changing it. A pre-0128 archive has no column and is accepted;
// a column that exists must open with the configuration key and hold a set that
// still verifies, so a restore never installs a tunnel no publication can render.
// It never returns the material itself.
func ValidateCanvasTunnelMaterial(ctx context.Context, database *sql.DB, configurationVault *vault.Vault) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0128_gost_tunnels.sql'").Scan(&applied); err != nil {
		return err
	}
	if applied == 0 {
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT id,gost_material FROM canvas_tunnels WHERE gost_material IS NOT NULL")
	if err != nil {
		return err
	}
	defer rows.Close()
	r := &Repository{Database: database, Vault: configurationVault}
	for rows.Next() {
		var tunnelID string
		var sealed []byte
		if err := rows.Scan(&tunnelID, &sealed); err != nil {
			return err
		}
		if _, err := r.openGostMaterial(tunnelID, sealed); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ValidateCanvasTunnels is read-only and also accepts pre-0124 archives. The
// recovery coordinator migrates and reconciles those archives before serving.
func ValidateCanvasTunnels(ctx context.Context, database *sql.DB) error {
	var applied, present int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0124_canvas_tunnels.sql'").Scan(&applied); err != nil {
		return err
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('canvas_tunnels','canvas_tunnel_objects','canvas_tunnel_publications','canvas_tunnel_reconcile')").Scan(&present); err != nil {
		return err
	}
	if present != applied*4 {
		return errors.New("canvas tunnel schema mismatch")
	}
	if applied == 0 {
		return nil
	}
	r := &Repository{Database: database}
	rows, err := database.QueryContext(ctx, "SELECT "+tunnelColumns+" FROM canvas_tunnels")
	if err != nil {
		return err
	}
	for rows.Next() {
		t, err := scanTunnel(rows)
		if err != nil {
			rows.Close()
			return err
		}
		c := t.connection
		c.Source = "source"
		c.Target = "target"
		c.TunnelID = ""
		layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "source", Ref: t.Source, Role: "entry"}, {InstanceID: "target", Ref: t.Target, Role: "exit"}}, Connections: []CanvasConnection{c}}
		if err := layout.validate(); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var invalid int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM canvas_tunnel_objects o LEFT JOIN canvas_tunnels t ON t.id=o.tunnel_id LEFT JOIN inbounds i ON o.object_kind='inbound' AND i.id=o.object_id LEFT JOIN outbounds b ON o.object_kind='outbound' AND b.id=o.object_id LEFT JOIN proxy_chains c ON o.object_kind='chain' AND c.id=o.object_id WHERE t.id IS NULL OR COALESCE(i.id,b.id,c.id) IS NULL`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errors.New("dangling canvas tunnel ownership")
	}
	schemes, _, err := r.CanvasSchemes(ctx, 1, 1000000)
	if err != nil {
		return err
	}
	for _, scheme := range schemes {
		if err := scheme.Layout.validate(); err != nil {
			return err
		}
	}
	return nil
}
