package certificate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"proxycontrol/internal/platform/vault"
)

// Repository owns the certificates table and the three system_settings rows of
// this feature. Nothing else writes them.
type Repository struct {
	Database *sql.DB
	Vault    *vault.Vault
}

// certificateColumns reads a row plus whether material is stored, without
// pulling the sealed blob for list queries.
const certificateColumns = "id,domains_json,status,not_before,not_after,last_error_code,last_attempt_at,next_attempt_at,attempts,auto_dns,coalesce(dns_node_id,''),encrypted_material IS NOT NULL,created_at,updated_at"

type rowScanner interface {
	Scan(...any) error
}

func scanCertificate(row rowScanner) (Certificate, error) {
	var c Certificate
	var domains string
	if err := row.Scan(&c.ID, &domains, &c.Status, &c.NotBefore, &c.NotAfter, &c.LastErrorCode, &c.LastAttemptAt,
		&c.NextAttemptAt, &c.Attempts, &c.AutoDNS, &c.DNSNodeID, &c.HasMaterial, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return Certificate{}, err
	}
	parsed, err := ParseDomains(domains)
	if err != nil {
		return Certificate{}, err
	}
	c.Domains = parsed
	return c, nil
}

func (r *Repository) List(ctx context.Context) ([]Certificate, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT "+certificateColumns+" FROM certificates ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Certificate{}
	for rows.Next() {
		c, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (Certificate, error) {
	c, err := scanCertificate(r.Database.QueryRowContext(ctx, "SELECT "+certificateColumns+" FROM certificates WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Certificate{}, ErrNotFound
	}
	return c, err
}

func (r *Repository) Create(ctx context.Context, c Certificate) error {
	domains, err := EncodeDomains(c.Domains)
	if err != nil {
		return err
	}
	var node any
	if c.DNSNodeID != "" {
		node = c.DNSNodeID
	}
	_, err = r.Database.ExecContext(ctx, "INSERT INTO certificates(id,domains_json,status,auto_dns,dns_node_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
		c.ID, domains, c.Status, c.AutoDNS, node, c.CreatedAt, c.UpdatedAt)
	return err
}

// MarkIssuing records that the worker started the attempt. The attempt counter
// only moves when the attempt fails, so a row being retried keeps its backoff
// step until the next failure.
func (r *Repository) MarkIssuing(ctx context.Context, id string, now time.Time) error {
	stamp := Stamp(now)
	_, err := r.Database.ExecContext(ctx, "UPDATE certificates SET status=?,last_attempt_at=?,updated_at=? WHERE id=?", StatusIssuing, stamp, stamp, id)
	return err
}

// SaveIssued stores the sealed material and clears the failure state.
func (r *Repository) SaveIssued(ctx context.Context, id string, material Material, notBefore, notAfter string, now time.Time) error {
	sealed, err := sealMaterial(r.Vault, material)
	if err != nil {
		return err
	}
	_, err = r.Database.ExecContext(ctx, `UPDATE certificates SET status=?,encrypted_material=?,not_before=?,not_after=?,last_error_code='',next_attempt_at='',attempts=0,updated_at=? WHERE id=?`,
		StatusValid, sealed, notBefore, notAfter, Stamp(now), id)
	return err
}

// RecordFailure stores the fixed code, counts the attempt and schedules the
// retry. keepValid leaves a certificate that is still serving its old material
// in the valid state, so an inbound keeps working while the renewal is retried.
func (r *Repository) RecordFailure(ctx context.Context, id, code string, attempts int, next string, keepValid bool, now time.Time) error {
	status := StatusFailed
	if keepValid {
		status = StatusValid
	}
	_, err := r.Database.ExecContext(ctx, "UPDATE certificates SET status=?,last_error_code=?,attempts=?,next_attempt_at=?,updated_at=? WHERE id=?",
		status, code, attempts, next, Stamp(now), id)
	return err
}

// MarkInterrupted turns the run a previous process left in flight into a failed
// row the normal backoff retries. A run cannot be resumed: its order, its TXT
// records and its account session are all gone.
func (r *Repository) MarkInterrupted(ctx context.Context, now time.Time) error {
	rows, err := r.Database.QueryContext(ctx, "SELECT id,attempts FROM certificates WHERE status=?", StatusIssuing)
	if err != nil {
		return err
	}
	type attempt struct {
		id       string
		attempts int
	}
	interrupted := []attempt{}
	for rows.Next() {
		var item attempt
		if err := rows.Scan(&item.id, &item.attempts); err != nil {
			rows.Close()
			return err
		}
		interrupted = append(interrupted, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range interrupted {
		if err := r.RecordFailure(ctx, item.id, CodeInterrupted, item.attempts+1, NextAttempt(now, item.attempts+1), false, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result, err := r.Database.ExecContext(ctx, "DELETE FROM certificates WHERE id=?", id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Usages maps a certificate ID to the inbounds that reference it. Phase D2 is
// what writes inbounds.certificate_id; the reference exists from 0137 so the
// store can already refuse a deletion that would strand an inbound.
func (r *Repository) Usages(ctx context.Context) (map[string][]Usage, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT certificate_id,id,name,node_id FROM inbounds WHERE certificate_id IS NOT NULL ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]Usage{}
	for rows.Next() {
		var certificateID string
		var usage Usage
		if err := rows.Scan(&certificateID, &usage.InboundID, &usage.Name, &usage.NodeID); err != nil {
			return nil, err
		}
		result[certificateID] = append(result[certificateID], usage)
	}
	return result, rows.Err()
}

// Material opens the stored certificate and key. An empty result means the row
// has not been issued yet.
func (r *Repository) Material(ctx context.Context, id string) (Material, error) {
	var sealed []byte
	if err := r.Database.QueryRowContext(ctx, "SELECT encrypted_material FROM certificates WHERE id=?", id).Scan(&sealed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Material{}, ErrNotFound
		}
		return Material{}, err
	}
	return openMaterial(r.Vault, sealed)
}

// NodeAddress reads the public address the Owner's node reports, which is where
// the automatic A/AAAA record must point. The connect host is not used: it may
// be the very domain being certified, so it cannot say where the node is.
func (r *Repository) NodeAddress(ctx context.Context, nodeID string) (string, error) {
	var address string
	err := r.Database.QueryRowContext(ctx, "SELECT public_ip FROM nodes WHERE id=? AND deleted_at IS NULL", nodeID).Scan(&address)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return address, err
}

// sealMaterial encrypts the certificate and key into the one column that holds
// them, so the JSON shape is decided here and nowhere else.
func sealMaterial(vault *vault.Vault, material Material) ([]byte, error) {
	data, err := json.Marshal(material)
	if err != nil {
		return nil, err
	}
	return vault.Seal(data)
}

func openMaterial(vault *vault.Vault, sealed []byte) (Material, error) {
	if len(sealed) == 0 {
		return Material{}, nil
	}
	plaintext, err := vault.Open(sealed)
	if err != nil {
		return Material{}, err
	}
	var material Material
	if err := json.Unmarshal(plaintext, &material); err != nil {
		return Material{}, err
	}
	return material, nil
}
