package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	"strings"
	"time"
)

type Repository struct {
	Database        *sql.DB
	Vault           *vault.Vault
	ConnectionNodes interface {
		RequireConnectionTargetTx(context.Context, *sql.Tx, string, string) error
		RequireConnectionLedgerTx(context.Context, *sql.Tx, string) error
		RequireConnectionGenerationTx(context.Context, *sql.Tx, string) error
		RequireConnectionFenceTx(context.Context, *sql.Tx, string) error
	}
	ConnectionGuard interface {
		RequireUserConnectionsPausedTx(context.Context, *sql.Tx, string) error
		RequireNodeConnectionsPausedTx(context.Context, *sql.Tx, string) error
	}
	// TrafficApplied is invoked after a traffic batch commits with the
	// distinct user IDs whose usage changed. Phase 5 uses it to re-evaluate
	// per-user quota without polling; it is optional and nil-safe.
	TrafficApplied func(context.Context, []string)
}

const userColumns = "traffic_reset_cycle,traffic_reset_anchor,traffic_reset_at,device_limit,login_enabled,id,username,display_name,status,expires_at,traffic_limit,used_traffic,max_devices,max_connections,protocols_json,remark,created_at,(SELECT count(*) FROM devices WHERE devices.user_id=users.id)"

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (User, error) {
	var user User
	var protocols string
	err := row.Scan(&user.TrafficResetCycle, &user.TrafficResetAnchor, &user.TrafficResetAt, &user.DeviceLimit, &user.LoginEnabled, &user.ID, &user.Username, &user.DisplayName, &user.Status, &user.ExpiresAt, &user.TrafficLimit, &user.UsedTraffic, &user.MaxDevices, &user.MaxConnections, &protocols, &user.Remark, &user.CreatedAt, &user.DevicesCount)
	if err != nil {
		return user, err
	}
	if err := json.Unmarshal([]byte(protocols), &user.AllowedProtocols); err != nil {
		return user, err
	}
	user.NodeGroupIDs = []string{}
	return user, nil
}
func (r *Repository) Groups(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT group_id FROM user_node_group_grants WHERE user_id=? ORDER BY group_id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		groups = append(groups, id)
	}
	return groups, rows.Err()
}
func (r *Repository) Get(ctx context.Context, userID string) (User, error) {
	user, err := scanUser(r.Database.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id=?", userID))
	if errors.Is(err, sql.ErrNoRows) {
		return user, ErrNotFound
	}
	if err != nil {
		return user, err
	}
	user.NodeGroupIDs, err = r.Groups(ctx, userID)
	return user, err
}

// ByUsername resolves an account without exposing the user table to portal authentication.
func (r *Repository) ByUsername(ctx context.Context, username string) (User, error) {
	var userID string
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM users WHERE username=?", username).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return r.Get(ctx, userID)
}
func (r *Repository) List(ctx context.Context, keyword string, page, size int) ([]User, int, error) {
	search := "%" + keyword + "%"
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE username LIKE ? OR display_name LIKE ?", search, search).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT "+userColumns+" FROM users WHERE username LIKE ? OR display_name LIKE ? ORDER BY created_at,id LIMIT ? OFFSET ?", search, search, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	items := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		items = append(items, user)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for index := range items {
		groups, err := r.Groups(ctx, items[index].ID)
		if err != nil {
			return nil, 0, err
		}
		items[index].NodeGroupIDs = groups
	}
	return items, count, nil
}
func setGrants(ctx context.Context, tx *sql.Tx, userID string, groups []string) error {
	if len(groups) > 100 {
		return errors.New("too many node group grants")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_node_group_grants WHERE user_id=?", userID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, group := range groups {
		if group == "" || seen[group] {
			return errors.New("invalid or duplicate node group grant")
		}
		seen[group] = true
		if _, err := tx.ExecContext(ctx, "INSERT INTO user_node_group_grants(user_id,group_id) VALUES(?,?)", userID, group); err != nil {
			return errors.New("node group does not exist")
		}
	}
	return nil
}
func (r *Repository) Save(ctx context.Context, userID string, input Input) (User, error) {
	if err := input.Validate(); err != nil {
		return User{}, err
	}
	created := timestamp(time.Now())
	create := userID == ""
	if create {
		var err error
		userID, err = id.New()
		if err != nil {
			return User{}, err
		}
	} else {
		existing, err := r.Get(ctx, userID)
		if err != nil {
			return User{}, err
		}
		if existing.Username != input.Username {
			return User{}, errors.New("username cannot be changed")
		}
	}
	protocols, _ := json.Marshal(input.AllowedProtocols)
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	// max_devices and max_connections are retired (plan C10): a new user gets
	// values the CHECK constraints accept and an update leaves them alone.
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO users(id,username,display_name,status,expires_at,traffic_limit,max_devices,max_connections,protocols_json,remark,created_at) VALUES(?,?,?,?,?,?,1,10000,?,?,?)", userID, input.Username, input.DisplayName, input.Status, input.ExpiresAt, input.TrafficLimit, string(protocols), input.Remark, created)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE users SET display_name=?,status=?,expires_at=?,traffic_limit=?,protocols_json=?,remark=? WHERE id=?", input.DisplayName, input.Status, input.ExpiresAt, input.TrafficLimit, string(protocols), input.Remark, userID)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return User{}, ErrConflict
		}
		return User{}, err
	}
	if err := setGrants(ctx, tx, userID, input.NodeGroupIDs); err != nil {
		return User{}, err
	}
	if input.TrafficResetAnchor == "" {
		if err := tx.QueryRowContext(ctx, "SELECT CASE WHEN traffic_reset_anchor<>'' AND traffic_reset_cycle=? THEN traffic_reset_anchor ELSE ? END FROM users WHERE id=?", input.TrafficResetCycle, created, userID).Scan(&input.TrafficResetAnchor); err != nil {
			return User{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET traffic_reset_at=CASE WHEN traffic_reset_cycle<>? OR traffic_reset_anchor<>? THEN '' ELSE traffic_reset_at END,traffic_reset_cycle=?,traffic_reset_anchor=? WHERE id=?", input.TrafficResetCycle, input.TrafficResetAnchor, input.TrafficResetCycle, input.TrafficResetAnchor, userID); err != nil {
		return User{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET device_limit=? WHERE id=?", input.DeviceLimit, userID); err != nil {
		return User{}, err
	}
	// The identity follows the allowed protocols in the same transaction, so a
	// user never exists without one and a protocol change issues and revokes
	// credentials atomically with it.
	if _, err := r.ensureAccessIdentityTx(ctx, tx, userID); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return r.Get(ctx, userID)
}
func (r *Repository) Delete(ctx context.Context, userID string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The nodes still run revisions that contain this user's credentials, so
	// their counters keep arriving. Keep the identity before the cascade removes
	// it; see traffic_tombstones.go.
	if err := recordUserCredentialTombstonesTx(ctx, tx, userID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id=?", userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return tx.Commit()
}
func (r *Repository) Grant(ctx context.Context, userID, groupID string, remove bool) error {
	if _, err := r.Get(ctx, userID); err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if remove {
		_, err = tx.ExecContext(ctx, "DELETE FROM user_node_group_grants WHERE user_id=? AND group_id=?", userID, groupID)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO user_node_group_grants(user_id,group_id) VALUES(?,?) ON CONFLICT DO NOTHING", userID, groupID)
	}
	if err != nil {
		return errors.New("node group does not exist")
	}
	return tx.Commit()
}
func (r *Repository) ResetTraffic(ctx context.Context, userID string) error {
	if _, err := r.Get(ctx, userID); err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET used_traffic=0 WHERE id=?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

const deviceColumns = "id,user_id,name,status,created_at,last_online_at,last_node_id,used_traffic"

func scanDevice(row scanner) (Device, error) {
	var device Device
	err := row.Scan(&device.ID, &device.UserID, &device.Name, &device.Status, &device.CreatedAt, &device.LastOnlineAt, &device.LastNodeID, &device.UsedTraffic)
	device.Credentials = []Credential{}
	return device, err
}
func (r *Repository) Device(ctx context.Context, deviceID string) (Device, error) {
	device, err := scanDevice(r.Database.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE id=?", deviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return device, ErrNotFound
	}
	if err != nil {
		return device, err
	}
	device.Credentials, err = r.Credentials(ctx, deviceID, false)
	return device, err
}
func (r *Repository) Devices(ctx context.Context, userID string, page, size int) ([]Device, int, error) {
	where := ""
	args := []any{}
	if userID != "" {
		where = " WHERE user_id=?"
		args = append(args, userID)
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM devices"+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any(nil), args...), size, (page-1)*size)
	rows, err := r.Database.QueryContext(ctx, "SELECT "+deviceColumns+" FROM devices"+where+" ORDER BY created_at,id LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	devices := []Device{}
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		devices = append(devices, device)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for index := range devices {
		credentials, err := r.Credentials(ctx, devices[index].ID, false)
		if err != nil {
			return nil, 0, err
		}
		devices[index].Credentials = credentials
	}
	return devices, count, nil
}
func (r *Repository) Credentials(ctx context.Context, deviceID string, reveal bool) ([]Credential, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT id,user_id,device_id,protocol,username,encrypted_secret,status,created_at FROM credentials WHERE device_id=? ORDER BY created_at,id", deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	credentials := []Credential{}
	for rows.Next() {
		var credential Credential
		var encoded []byte
		if err := rows.Scan(&credential.ID, &credential.UserID, &credential.DeviceID, &credential.Protocol, &credential.Username, &encoded, &credential.Status, &credential.CreatedAt); err != nil {
			return nil, err
		}
		if reveal && credential.Status == "active" {
			plaintext, err := r.Vault.Open(encoded)
			if err != nil {
				return nil, err
			}
			var secret Secret
			if err := json.Unmarshal(plaintext, &secret); err != nil {
				return nil, err
			}
			credential.Secret, credential.PublicKey = secret.Value, secret.PublicKey
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}

// CreateDevice is the v1.0 entry point for issuing credentials. Since v1.1 a
// user has exactly one access identity, so it no longer adds a device: it checks
// the request as before, makes sure the identity holds the user's credentials
// and returns it, with the credentials of the requested protocols revealed the
// way a newly created device returned them. Internal callers and fixtures use
// it; the HTTP route is gone (410).
func (r *Repository) CreateDevice(ctx context.Context, userID string, input DeviceInput) (Device, error) {
	if err := ValidateProtocols(input.Protocols); err != nil {
		return Device{}, err
	}
	user, err := r.Get(ctx, userID)
	if err != nil {
		return Device{}, err
	}
	if !user.Active(time.Now()) {
		return Device{}, ErrInactive
	}
	allowed := map[string]bool{}
	for _, protocol := range user.AllowedProtocols {
		allowed[protocol] = true
	}
	for _, protocol := range input.Protocols {
		if !allowed[protocol] {
			return Device{}, errors.New("protocol not allowed for this user")
		}
	}
	if _, err := r.EnsureAccessIdentity(ctx, userID); err != nil {
		return Device{}, err
	}
	identity, err := r.AccessIdentity(ctx, userID)
	if err != nil {
		return Device{}, err
	}
	revealed, err := r.Credentials(ctx, identity.ID, true)
	if err != nil {
		return Device{}, err
	}
	requested := map[string]bool{}
	for _, protocol := range input.Protocols {
		requested[protocol] = true
	}
	identity.Credentials = []Credential{}
	for _, credential := range revealed {
		if credential.Status == "active" && requested[credential.Protocol] {
			identity.Credentials = append(identity.Credentials, credential)
		}
	}
	return identity, nil
}
func (r *Repository) addCredential(ctx context.Context, tx *sql.Tx, deviceID, userID, protocol string) (Credential, error) {
	credentialID, err := id.New()
	if err != nil {
		return Credential{}, err
	}
	secret, err := GenerateCredential(protocol)
	if err != nil {
		return Credential{}, err
	}
	plaintext, _ := json.Marshal(secret)
	sealed, err := r.Vault.Seal(plaintext)
	if err != nil {
		return Credential{}, err
	}
	credential := Credential{ID: credentialID, UserID: userID, DeviceID: deviceID, Protocol: protocol, Status: "active", CreatedAt: timestamp(time.Now()), Secret: secret.Value, PublicKey: secret.PublicKey}
	credential.Username = secret.Username
	if protocol == "http" || protocol == "socks5" {
		credential.Username = "device-" + deviceID[:12]
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO credentials(id,user_id,device_id,protocol,username,encrypted_secret,status,created_at) VALUES(?,?,?,?,?,?,'active',?)", credential.ID, userID, deviceID, protocol, credential.Username, sealed, credential.CreatedAt); err != nil {
		return Credential{}, err
	}
	return credential, nil
}
func (r *Repository) UpdateDevice(ctx context.Context, deviceID string, input DeviceInput) error {
	device, err := r.Device(ctx, deviceID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 240 || input.Status != "active" && input.Status != "disabled" {
		return errors.New("invalid device name or status")
	}
	return r.changeDeviceTx(ctx, deviceID, device.UserID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE devices SET name=?,status=? WHERE id=?", strings.TrimSpace(input.Name), input.Status, deviceID)
		return err
	})
}
func (r *Repository) DeleteDevice(ctx context.Context, deviceID string) error {
	device, err := r.Device(ctx, deviceID)
	if err != nil {
		return err
	}
	return r.changeDeviceTx(ctx, deviceID, device.UserID, func(tx *sql.Tx) error {
		// The node may still run a revision that contains this device's
		// credentials, so keep their identity before the cascade removes it.
		if err := recordDeviceCredentialTombstonesTx(ctx, tx, deviceID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM devices WHERE id=?", deviceID)
		return err
	})
}

func (r *Repository) changeDeviceTx(ctx context.Context, deviceID, userID string, change func(*sql.Tx) error) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	if err := tx.QueryRowContext(ctx, "SELECT user_id FROM devices WHERE id=?", deviceID).Scan(&owner); err != nil {
		return err
	}
	if owner != userID {
		return ErrCredential
	}
	if err := change(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) RotateCredential(ctx context.Context, deviceID, protocol string, revoke bool) (Credential, error) {
	device, err := r.Device(ctx, deviceID)
	if err != nil {
		return Credential{}, err
	}
	if err := ValidateProtocols([]string{protocol}); err != nil {
		return Credential{}, err
	}
	user, err := r.Get(ctx, device.UserID)
	if err != nil {
		return Credential{}, err
	}
	allowed := false
	for _, value := range user.AllowedProtocols {
		if value == protocol {
			allowed = true
		}
	}
	if !revoke && !allowed {
		return Credential{}, errors.New("protocol not allowed for this user")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Credential{}, err
	}
	defer tx.Rollback()
	var owner string
	if err := tx.QueryRowContext(ctx, "SELECT user_id FROM devices WHERE id=?", deviceID).Scan(&owner); err != nil {
		return Credential{}, err
	}
	if owner != device.UserID {
		return Credential{}, ErrCredential
	}
	if _, err := tx.ExecContext(ctx, "UPDATE credentials SET status='revoked' WHERE device_id=? AND protocol=? AND status='active'", deviceID, protocol); err != nil {
		return Credential{}, err
	}
	result := Credential{}
	if !revoke {
		result, err = r.addCredential(ctx, tx, deviceID, device.UserID, protocol)
		if err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
