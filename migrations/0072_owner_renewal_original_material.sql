ALTER TABLE owner_expired_certificate_renewals ADD COLUMN previous_certificate_pem TEXT NOT NULL DEFAULT '';
ALTER TABLE owner_expired_certificate_renewals ADD COLUMN previous_expires_at TEXT NOT NULL DEFAULT '';
ALTER TABLE owner_expired_certificate_renewals ADD COLUMN signing_ca_pem TEXT NOT NULL DEFAULT '';
ALTER TABLE owner_expired_certificate_renewals ADD COLUMN issuance_proof_json TEXT NOT NULL DEFAULT '';
