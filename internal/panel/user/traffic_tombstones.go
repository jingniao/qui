package user

import (
	"context"
	"database/sql"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// A deleted user or device removes the rows a counter is charged to, but the
// node keeps running the revision that still contained their credentials, so
// the counter keeps arriving. The tombstones recorded in the same transaction as
// the deletion keep the identity, which lets applyTraffic accept such a counter
// without resurrecting the deleted user or device.
func recordUserCredentialTombstonesTx(ctx context.Context, tx *sql.Tx, userID string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO credential_tombstones(credential_id,user_id,device_id,deleted_at) SELECT id,user_id,device_id,? FROM credentials WHERE user_id=? ON CONFLICT(credential_id) DO NOTHING", timestamp(time.Now()), userID)
	return err
}

func recordDeviceCredentialTombstonesTx(ctx context.Context, tx *sql.Tx, deviceID string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO credential_tombstones(credential_id,user_id,device_id,deleted_at) SELECT id,user_id,device_id,? FROM credentials WHERE device_id=? ON CONFLICT(credential_id) DO NOTHING", timestamp(time.Now()), deviceID)
	return err
}

// credentialTombstoned reports whether exactly this credential identity was
// deleted. A counter that merely reuses the credential ID of a different user
// or device is not covered.
func credentialTombstoned(ctx context.Context, tx *sql.Tx, counter traffic.Counter) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM credential_tombstones WHERE credential_id=? AND user_id=? AND device_id=?", counter.CredentialID, counter.UserID, counter.DeviceID).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

// revisionHeldIdentity reports whether the revision the counter was generated
// under still contained this identity. Without that evidence a missing row is an
// unknown identity, not a deletion.
func revisionHeldIdentity(states map[int64]desiredstate.State, counter traffic.Counter) bool {
	state, known := states[counter.Revision]
	if !known {
		return false
	}
	for _, credential := range state.Credentials {
		if credential.ID == counter.CredentialID && credential.UserID == counter.UserID && credential.DeviceID == counter.DeviceID && credential.Protocol == counter.Protocol && !credential.Internal {
			return true
		}
	}
	return false
}

// deletedIdentityAccepted is the single gate before a counter whose user or
// device row is gone may be accounted: the identity must have been deleted, and
// the revision named by the counter must have contained exactly that identity.
func deletedIdentityAccepted(ctx context.Context, tx *sql.Tx, states map[int64]desiredstate.State, counter traffic.Counter) (bool, error) {
	if !revisionHeldIdentity(states, counter) {
		return false, nil
	}
	return credentialTombstoned(ctx, tx, counter)
}
