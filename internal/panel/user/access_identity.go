package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"proxycontrol/internal/platform/id"
)

// Since v1.1 a user has exactly one device, the hidden access identity (plan
// section 4.1). It is never shown or managed as a device: Owners and users only
// see the user. Its ID still flows to the Agents as DeviceID, so the traffic,
// tombstone and quota paths and every Agent contract stay unchanged.

// ErrDevicesRemoved reports a call to a device API that v1.1 removed.
var ErrDevicesRemoved = errors.New("devices are managed per user since v1.1")

// identityName is the name of an identity the panel had to create itself.
const identityName = "access"

// identityOrder picks the device a user keeps, as migration 0113 does: the one
// with the active subscription token, else the oldest active one, else the
// oldest.
const identityOrder = "EXISTS(SELECT 1 FROM subscription_tokens t WHERE t.device_id=devices.id AND t.revoked_at IS NULL) DESC, status='active' DESC, created_at, id"

// AccessIdentity returns the user's access identity, or ErrNotFound when the
// user has none yet (EnsureAccessIdentity creates it).
func (r *Repository) AccessIdentity(ctx context.Context, userID string) (Device, error) {
	var deviceID string
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM devices WHERE user_id=? ORDER BY "+identityOrder+" LIMIT 1", userID).Scan(&deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	return r.Device(ctx, deviceID)
}

// EnsureAccessIdentity makes the user's identity exist and hold exactly one
// active credential per allowed protocol: a missing one is issued, one for a
// protocol the user no longer may use is revoked, and any second device (only
// possible from a database older than migration 0113 restored without it) is
// removed with its credentials tombstoned. It reports whether anything changed,
// so the caller can queue the automatic revision.
func (r *Repository) EnsureAccessIdentity(ctx context.Context, userID string) (bool, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	changed, err := r.ensureAccessIdentityTx(ctx, tx, userID)
	if err != nil {
		return false, err
	}
	return changed, tx.Commit()
}

func (r *Repository) ensureAccessIdentityTx(ctx context.Context, tx *sql.Tx, userID string) (bool, error) {
	var protocolsJSON string
	if err := tx.QueryRowContext(ctx, "SELECT protocols_json FROM users WHERE id=?", userID).Scan(&protocolsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	var protocols []string
	if err := json.Unmarshal([]byte(protocolsJSON), &protocols); err != nil {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,status FROM devices WHERE user_id=? ORDER BY "+identityOrder, userID)
	if err != nil {
		return false, err
	}
	type device struct{ id, status string }
	devices := []device{}
	for rows.Next() {
		var item device
		if err := rows.Scan(&item.id, &item.status); err != nil {
			rows.Close()
			return false, err
		}
		devices = append(devices, item)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	changed := false
	if len(devices) == 0 {
		deviceID, err := id.New()
		if err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO devices(id,user_id,name,status,created_at) VALUES(?,?,?,'active',?)", deviceID, userID, identityName, timestamp(time.Now())); err != nil {
			return false, err
		}
		devices = append(devices, device{id: deviceID, status: "active"})
		changed = true
	}
	identity := devices[0]
	for _, extra := range devices[1:] {
		if err := recordDeviceCredentialTombstonesTx(ctx, tx, extra.id); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM devices WHERE id=?", extra.id); err != nil {
			return false, err
		}
		changed = true
	}
	if identity.status != "active" {
		// A device could be disabled before v1.1. The user's own status now
		// decides access; a disabled identity would silently lock the user out.
		if _, err := tx.ExecContext(ctx, "UPDATE devices SET status='active' WHERE id=?", identity.id); err != nil {
			return false, err
		}
		changed = true
	}
	allowed := map[string]bool{}
	for _, protocol := range protocols {
		allowed[protocol] = true
	}
	active := map[string]bool{}
	credentials, err := tx.QueryContext(ctx, "SELECT protocol FROM credentials WHERE device_id=? AND status='active'", identity.id)
	if err != nil {
		return false, err
	}
	for credentials.Next() {
		var protocol string
		if err := credentials.Scan(&protocol); err != nil {
			credentials.Close()
			return false, err
		}
		active[protocol] = true
	}
	if err := credentials.Close(); err != nil {
		return false, err
	}
	for protocol := range active {
		if allowed[protocol] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE credentials SET status='revoked' WHERE device_id=? AND protocol=? AND status='active'", identity.id, protocol); err != nil {
			return false, err
		}
		changed = true
	}
	for _, protocol := range protocols {
		if active[protocol] {
			continue
		}
		if _, err := r.addCredential(ctx, tx, identity.id, userID, protocol); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// ResetCredentials re-issues every credential of the user's identity. The old
// proxy settings keep working until the automatic revision that carries the new
// ones has been applied on the nodes; the subscription serves the new ones as
// soon as a node runs them.
func (r *Repository) ResetCredentials(ctx context.Context, userID string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := r.ensureAccessIdentityTx(ctx, tx, userID); err != nil {
		return err
	}
	var deviceID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM devices WHERE user_id=?", userID).Scan(&deviceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE credentials SET status='revoked' WHERE device_id=? AND status='active'", deviceID); err != nil {
		return err
	}
	if _, err := r.ensureAccessIdentityTx(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// EnsureAllAccessIdentities runs EnsureAccessIdentity for every user. The panel
// calls it at startup, which also covers a restored database (a restore
// restarts the service instance). It returns the users whose identity changed.
func (r *Repository) EnsureAllAccessIdentities(ctx context.Context) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, userID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	changed := []string{}
	for _, userID := range ids {
		updated, err := r.EnsureAccessIdentity(ctx, userID)
		if err != nil {
			return changed, err
		}
		if updated {
			changed = append(changed, userID)
		}
	}
	return changed, nil
}
