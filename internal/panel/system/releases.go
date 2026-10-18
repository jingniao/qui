package system

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/release"
)

type ReleaseEntry = release.CatalogueEntry

func scanRelease(row interface{ Scan(...any) error }) (ReleaseEntry, error) {
	var entry ReleaseEntry
	var data []byte
	if err := row.Scan(&entry.ID, &data, &entry.CreatedBy, &entry.CreatedAt); err != nil {
		return entry, err
	}
	signed, err := release.Decode(data)
	entry.Signed = signed
	return entry, err
}
func (r *Repository) ReleaseTx(ctx context.Context, tx *sql.Tx, entryID string) (ReleaseEntry, error) {
	return scanRelease(tx.QueryRowContext(ctx, "SELECT id,signed_json,created_by,created_at FROM release_catalogue WHERE id=?", entryID))
}
func (r *Repository) SaveRelease(ctx context.Context, signed release.Signed, key ed25519.PublicKey, author, ip string) (ReleaseEntry, error) {
	if author == "" || len(author) > 128 || len(key) != ed25519.PublicKeySize || release.KeyID(key) != signed.KeyID {
		return ReleaseEntry{}, release.ErrSignature
	}
	manifest := signed.Manifest
	if err := signed.Verify(release.Policy{Component: manifest.Component, OS: manifest.OS, Architecture: manifest.Architecture, Now: time.Now().UTC(), Keys: map[string]release.TrustedKey{signed.KeyID: {PublicKey: key, Components: []string{manifest.Component}}}}); err != nil {
		return ReleaseEntry{}, err
	}
	data, err := json.Marshal(signed)
	if err != nil {
		return ReleaseEntry{}, err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return ReleaseEntry{}, err
	}
	defer tx.Rollback()
	m := signed.Manifest
	old, err := scanRelease(tx.QueryRowContext(ctx, "SELECT id,signed_json,created_by,created_at FROM release_catalogue WHERE component=? AND operating_system=? AND architecture=? AND release_sequence=?", m.Component, m.OS, m.Architecture, m.Sequence))
	if err == nil {
		if old.Signed.Manifest != m {
			return ReleaseEntry{}, release.ErrPolicy
		}
		if old.Signed == signed {
			return old, nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE release_catalogue SET signed_json=?,signing_public_key=? WHERE id=?", data, base64.StdEncoding.EncodeToString(key), old.ID); err != nil {
			return old, err
		}
		if err := r.RecordTx(ctx, tx, author, "rotate_release_signature", old.ID, "成功", ip); err != nil {
			return old, err
		}
		old.Signed = signed
		return old, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ReleaseEntry{}, err
	}
	entryID, err := id.New()
	if err != nil {
		return ReleaseEntry{}, err
	}
	entry := ReleaseEntry{ID: entryID, Signed: signed, CreatedBy: author, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := tx.ExecContext(ctx, "INSERT INTO release_catalogue(id,component,operating_system,architecture,release_sequence,version,digest,signed_json,signing_public_key,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", entry.ID, m.Component, m.OS, m.Architecture, m.Sequence, m.Version, m.SHA256, data, base64.StdEncoding.EncodeToString(key), author, entry.CreatedAt); err != nil {
		return entry, err
	}
	if err := r.RecordTx(ctx, tx, author, "register_release", entry.ID, "成功", ip); err != nil {
		return entry, err
	}
	return entry, tx.Commit()
}
func (r *Repository) ListReleases(ctx context.Context, page, size int) ([]ReleaseEntry, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM release_catalogue").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,signed_json,created_by,created_at FROM release_catalogue ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []ReleaseEntry{}
	for rows.Next() {
		entry, err := scanRelease(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, entry)
	}
	return items, total, rows.Err()
}
func (r *Repository) RecordTx(ctx context.Context, tx *sql.Tx, actor, action, target, result, ip string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,?,?)", time.Now().UTC().Format(time.RFC3339Nano), actor, action, target, result, ip)
	return err
}
