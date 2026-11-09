package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"proxycontrol/internal/panel/certificate"
	"proxycontrol/internal/platform/vault"
)

// validateCertificates enforces the 0137 table and its stored values. The
// certificate material is sealed with the configuration key, so a candidate that
// cannot open it would restore a certificate phase D2 could never render; that is
// the same reason the GOST tunnel material is checked here.
func validateCertificates(ctx context.Context, db *sql.DB, configurationVault *vault.Vault) error {
	var applied, present int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0137_certificates.sql'").Scan(&applied) != nil ||
		db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='certificates'").Scan(&present) != nil ||
		present != applied {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	// The reference column arrives with the same migration, so an archive that
	// applied it must also carry the column phase D2 writes.
	var column int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('inbounds') WHERE name='certificate_id'").Scan(&column) != nil || column != 1 {
		return ErrArchive
	}
	rows, err := db.QueryContext(ctx, "SELECT id,domains_json,status,not_before,not_after,last_error_code,last_attempt_at,next_attempt_at,attempts,auto_dns,CASE WHEN length(encrypted_material)<=1048576 THEN encrypted_material END,created_at,updated_at FROM certificates")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var id, domains, status, notBefore, notAfter, code, lastAttempt, nextAttempt, createdAt, updatedAt string
		var attempts, autoDNS int
		var sealed []byte
		if rows.Scan(&id, &domains, &status, &notBefore, &notAfter, &code, &lastAttempt, &nextAttempt, &attempts, &autoDNS, &sealed, &createdAt, &updatedAt) != nil {
			rows.Close()
			return ErrArchive
		}
		if id == "" || !validCertificateRow(status, code, attempts, autoDNS) || !utcTimestamp(createdAt) || !utcTimestamp(updatedAt) {
			rows.Close()
			return ErrArchive
		}
		if _, err := certificate.ParseStoredDomains(domains); err != nil {
			rows.Close()
			return ErrArchive
		}
		for _, value := range []string{notBefore, notAfter, lastAttempt, nextAttempt} {
			if value != "" && !utcTimestamp(value) {
				rows.Close()
				return ErrArchive
			}
		}
		if len(sealed) > 0 && !openableMaterial(configurationVault, sealed) {
			rows.Close()
			return ErrArchive
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	// A reference to a certificate the archive does not hold would restore an
	// inbound pointing at nothing.
	var orphan int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM inbounds WHERE certificate_id IS NOT NULL AND certificate_id NOT IN (SELECT id FROM certificates)").Scan(&orphan); err != nil || orphan != 0 {
		return ErrArchive
	}
	return nil
}

func validCertificateRow(status, code string, attempts, autoDNS int) bool {
	switch status {
	case certificate.StatusPending, certificate.StatusIssuing, certificate.StatusValid, certificate.StatusFailed:
	default:
		return false
	}
	if code != "" && !certificate.KnownErrorCode(code) {
		return false
	}
	return attempts >= 0 && (autoDNS == 0 || autoDNS == 1)
}

// openableMaterial reports whether the sealed blob decrypts into a certificate
// and key pair this panel could have written.
func openableMaterial(configurationVault *vault.Vault, sealed []byte) bool {
	plaintext, err := configurationVault.Open(sealed)
	if err != nil {
		return false
	}
	var material certificate.Material
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&material) != nil {
		return false
	}
	return material.CertificatePEM != "" && material.PrivateKeyPEM != ""
}
