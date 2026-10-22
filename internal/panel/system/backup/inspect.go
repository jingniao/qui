package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/access"
	nodeledger "proxycontrol/internal/panel/node"
	userledger "proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/migrations"
	"proxycontrol/pkg/contracts/credential"
	"proxycontrol/pkg/contracts/desiredstate"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

func decodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return manifest, ErrArchive
	}
	if manifest.Version != 1 || !validID(manifest.ID) || manifest.CreatedAt.IsZero() || manifest.CreatedAt.Year() < 2000 || manifest.CreatedAt.Year() > 9999 || manifest.CreatedBy == "" || len(manifest.CreatedBy) > 128 || len(manifest.Files) < 5 || len(manifest.Files) > maxFiles {
		return manifest, ErrArchive
	}
	seen := map[string]bool{}
	var total int64
	for _, file := range manifest.Files {
		digest, err := hex.DecodeString(file.SHA256)
		if !validName(file.Name) || seen[file.Name] || file.Bytes < 0 || file.Bytes > fileLimit(file.Name) || err != nil || len(digest) != sha256.Size {
			return manifest, ErrArchive
		}
		seen[file.Name] = true
		total += file.Bytes
		if total > MaxUnpackedBytes {
			return manifest, ErrArchive
		}
	}
	for _, name := range []string{"database/panel.db", "configuration/panel.yaml", "identity/ca.crt", "identity/ca.key", "identity/configuration.key"} {
		if !seen[name] {
			return manifest, ErrArchive
		}
	}
	return manifest, nil
}

// Inspect verifies a complete archive into an isolated protected directory.
// It does not replace a live database or start restored services.
func Inspect(ctx context.Context, directory, archivePath, password string) (result Result, resultErr error) {
	return inspect(ctx, directory, archivePath, password, nil)
}

// Examine invokes trusted read-only consumers only after complete file and
// database verification. The temporary path is valid only inside the callback.
func Examine(ctx context.Context, directory, archivePath, password string, read func(Result, string) error) (Result, error) {
	if read == nil {
		return Result{}, ErrArchive
	}
	return inspect(ctx, directory, archivePath, password, read)
}

// ValidateMaterialSet checks database/key/certificate relationships in an
// isolated material set. It does not authenticate its provenance; the recovery
// coordinator must also pin and verify its manifest and every file digest.
func ValidateMaterialSet(ctx context.Context, directory string) error {
	return validateContents(ctx, directory)
}

func inspect(ctx context.Context, directory, archivePath, password string, read func(Result, string) error) (result Result, resultErr error) {
	return inspectWithValidation(ctx, directory, archivePath, password, read, validateContents)
}

func inspectWithValidation(ctx context.Context, directory, archivePath, password string, read func(Result, string) error, validate func(context.Context, string) error) (result Result, resultErr error) {
	if err := ValidatePassword(password); err != nil {
		return result, err
	}
	info, err := os.Lstat(archivePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() < headerBytes || info.Size() > MaxArchiveBytes {
		return result, ErrArchive
	}
	work, err := newWork(directory)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, removeWork(directory, work))
		if resultErr != nil {
			resultErr = errors.Join(resultErr, ctx.Err())
		}
	}()
	file, err := os.Open(archivePath)
	if err != nil {
		return result, err
	}
	defer file.Close()
	hash := sha256.New()
	sealed, err := newEncryptedReader(ctx, io.TeeReader(contextReader{ctx, file}, hash), password)
	if err != nil {
		return result, err
	}
	compressed, err := gzip.NewReader(sealed)
	if err != nil {
		return result, ErrArchive
	}
	defer compressed.Close()
	compressed.Multistream(false)
	archive := tar.NewReader(compressed)
	header, err := archive.Next()
	if err != nil || header.Name != "manifest.json" || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > maxManifestBytes {
		return result, ErrArchive
	}
	data, err := io.ReadAll(io.LimitReader(archive, maxManifestBytes+1))
	if err != nil {
		return result, ErrArchive
	}
	manifest, err := decodeManifest(data)
	if err != nil {
		return result, err
	}
	expected := map[string]File{}
	for _, file := range manifest.Files {
		expected[file.Name] = file
	}
	root, err := os.OpenRoot(work)
	if err != nil {
		return result, err
	}
	defer root.Close()
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil || header.Typeflag != tar.TypeReg || !validName(header.Name) {
			return result, ErrArchive
		}
		item, exists := expected[header.Name]
		if !exists || header.Size != item.Bytes {
			return result, ErrArchive
		}
		if err := root.MkdirAll(filepath.Dir(header.Name), 0700); err != nil {
			return result, err
		}
		output, err := root.OpenFile(header.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return result, err
		}
		digest := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(output, digest), contextReader{ctx, archive})
		closeErr := output.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return result, err
		}
		if n != item.Bytes || hex.EncodeToString(digest.Sum(nil)) != item.SHA256 {
			return result, ErrArchive
		}
		delete(expected, item.Name)
	}
	if len(expected) != 0 {
		return result, ErrArchive
	}
	var extra [1]byte
	if _, err := io.ReadFull(compressed, extra[:]); err != io.EOF {
		return result, ErrArchive
	}
	if _, err := io.ReadFull(sealed, extra[:]); err != io.EOF || !sealed.finished {
		return result, ErrArchive
	}
	if err := validate(ctx, work); err != nil {
		return result, err
	}
	result = Result{Manifest: manifest, Bytes: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}
	if read != nil {
		if err := read(result, work); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func readonlyDatabase(databasePath string) (*sql.DB, error) {
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{"mode": {"ro"}, "immutable": {"1"}, "_pragma": {"query_only(1)", "foreign_keys(1)"}}
	uri.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", uri.String())
	if err == nil {
		database.SetMaxOpenConns(1)
		database.SetMaxIdleConns(1)
	}
	return database, err
}

func validateContents(ctx context.Context, work string) error {
	key, err := os.ReadFile(filepath.Join(work, "identity", "configuration.key"))
	if err != nil {
		return ErrArchive
	}
	configurationVault, err := vault.FromKey(key)
	clear(key)
	if err != nil {
		return ErrArchive
	}
	caPEM, err := os.ReadFile(filepath.Join(work, "identity", "ca.crt"))
	if err != nil {
		return ErrArchive
	}
	caKey, err := os.ReadFile(filepath.Join(work, "identity", "ca.key"))
	if err != nil {
		return ErrArchive
	}
	defer clear(caKey)
	pair, err := tls.X509KeyPair(caPEM, caKey)
	if err != nil {
		return ErrArchive
	}
	ca, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || !ca.IsCA {
		return ErrArchive
	}
	caPrivate, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || caPrivate.Curve != elliptic.P256() {
		return ErrArchive
	}
	archivedAuthority, err := pki.LoadExisting(filepath.Join(work, "identity"))
	if err != nil {
		return ErrArchive
	}
	// Optional development server materials must form one valid pair signed
	// by the archived node CA; never silently regenerate an incomplete pair.
	serverCertificate, certErr := os.ReadFile(filepath.Join(work, "identity", "server.crt"))
	serverKey, keyErr := os.ReadFile(filepath.Join(work, "identity", "server.key"))
	defer clear(serverKey)
	if !(errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist)) {
		if certErr != nil || keyErr != nil {
			return ErrArchive
		}
		serverPair, err := tls.X509KeyPair(serverCertificate, serverKey)
		if err != nil {
			return ErrArchive
		}
		leaf, err := x509.ParseCertificate(serverPair.Certificate[0])
		if err != nil {
			return ErrArchive
		}
		trusted := leaf.CheckSignatureFrom(ca) == nil
		if archivedAuthority != nil {
			trusted = archivedAuthority.IssuedByTrustedCA(leaf)
		}
		if !trusted {
			return ErrArchive
		}
	}
	configuration, err := os.ReadFile(filepath.Join(work, "configuration", "panel.yaml"))
	if err != nil {
		return ErrArchive
	}
	var panel config.Panel
	decoder := yaml.NewDecoder(bytes.NewReader(configuration))
	decoder.KnownFields(true)
	if err := decoder.Decode(&panel); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ErrArchive
	}
	panel.Database = filepath.Join(work, "database", "panel.db")
	panel.NodeIdentityDirectory = filepath.Join(work, "identity")
	if !panel.Development {
		panel.TLS = stagedTLS(work, panel.TLS.ClientCA != "")
	}
	if err := panel.Validate(); err != nil {
		return ErrArchive
	}
	database, err := readonlyDatabase(panel.Database)
	if err != nil {
		return ErrArchive
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return ErrArchive
	}
	checks := 0
	for rows.Next() {
		var check string
		if err := rows.Scan(&check); err != nil || check != "ok" {
			rows.Close()
			return ErrArchive
		}
		checks++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || checks != 1 {
		return ErrArchive
	}
	rows, err = database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrArchive
	}
	invalid := rows.Next()
	err = rows.Err()
	rows.Close()
	if invalid || err != nil {
		return ErrArchive
	}
	if err := validateMigrations(ctx, database); err != nil {
		return err
	}
	for _, table := range []string{"admins", "users", "nodes", "node_groups", "policies", "service_sets", "config_revisions"} {
		var count int64
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return ErrArchive
		}
	}
	for _, item := range []struct {
		table, field, kind string
		limit              int
	}{{"credentials", "encrypted_secret", "credential", 16384}, {"relay_credentials", "encrypted_secret", "relay", 16384}, {"inbounds", "encrypted_settings", "settings", (2 << 20) + 64}, {"outbounds", "encrypted_settings", "settings", (2 << 20) + 64}, {"config_revisions", "encrypted_state", "state", (2 << 20) + 64}} {
		// CASE refuses an oversized stored blob before the driver allocates it.
		query := "SELECT id,CASE WHEN length(" + item.field + ")<=" + strconv.Itoa(item.limit) + " THEN " + item.field + " END FROM " + item.table
		rows, err := database.QueryContext(ctx, query)
		if err != nil {
			return ErrArchive
		}
		for rows.Next() {
			var recordID string
			var encrypted []byte
			if err := rows.Scan(&recordID, &encrypted); err != nil {
				rows.Close()
				return ErrArchive
			}
			plaintext, err := configurationVault.Open(encrypted)
			if err != nil {
				rows.Close()
				return ErrArchive
			}
			if item.kind == "state" {
				var state desiredstate.State
				state, err = desiredstate.Decode(plaintext)
				if err == nil && strconv.FormatInt(state.Revision, 10) != recordID {
					err = ErrArchive
				}
			} else if item.kind == "settings" {
				var settings desiredstate.ProtocolSettings
				decoder := json.NewDecoder(bytes.NewReader(plaintext))
				decoder.DisallowUnknownFields()
				if err = decoder.Decode(&settings); err == nil && decoder.Decode(&struct{}{}) != io.EOF {
					err = ErrArchive
				}
			} else if item.kind == "credential" {
				var secret credential.Secret
				decoder := json.NewDecoder(bytes.NewReader(plaintext))
				decoder.DisallowUnknownFields()
				if err = decoder.Decode(&secret); err == nil && (decoder.Decode(&struct{}{}) != io.EOF || secret.Value == "" || len(secret.Value) > 4096 || !utf8.ValidString(secret.Value)) {
					err = ErrArchive
				}
				if err == nil && secret.Username != "" {
					err = desiredstate.ValidateUUID(secret.Username)
				}
			} else if len(plaintext) == 0 || len(plaintext) > 4096 || !utf8.Valid(plaintext) {
				err = ErrArchive
			}
			clear(plaintext)
			if err != nil {
				rows.Close()
				return ErrArchive
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrArchive
		}
	}
	rows, err = database.QueryContext(ctx, "SELECT certificate_pem,fingerprint FROM node_identities")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var data, fingerprint string
		if err := rows.Scan(&data, &fingerprint); err != nil {
			rows.Close()
			return ErrArchive
		}
		block, rest := pem.Decode([]byte(data))
		if block == nil || len(bytes.TrimSpace(rest)) != 0 {
			rows.Close()
			return ErrArchive
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !authenticatesArchivedCertificate(archivedAuthority, ca, certificate) {
			rows.Close()
			return ErrArchive
		}
		digest := sha256.Sum256(block.Bytes)
		if fingerprint != hex.EncodeToString(digest[:]) {
			rows.Close()
			return ErrArchive
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	var renewalTable int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='node_certificate_renewals'").Scan(&renewalTable); err != nil {
		return ErrArchive
	}
	if renewalTable == 1 {
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_certificate_renewals p LEFT JOIN node_identities i ON i.node_id=p.node_id WHERE i.node_id IS NULL").Scan(&renewalTable); err != nil || renewalTable != 0 {
			return ErrArchive
		}
		rows, err := database.QueryContext(ctx, "SELECT p.node_id,p.previous_fingerprint,CASE WHEN length(p.certificate_pem)<=1048576 THEN p.certificate_pem END,p.fingerprint,i.fingerprint,p.expires_at,i.certificate_pem FROM node_certificate_renewals p JOIN node_identities i ON i.node_id=p.node_id")
		if err != nil {
			return ErrArchive
		}
		for rows.Next() {
			var nodeID, previous, encoded, fingerprint, current, expiry, currentPEM string
			if err := rows.Scan(&nodeID, &previous, &encoded, &fingerprint, &current, &expiry, &currentPEM); err != nil {
				rows.Close()
				return ErrArchive
			}
			block, rest := pem.Decode([]byte(encoded))
			if block == nil || len(bytes.TrimSpace(rest)) != 0 || previous != current {
				rows.Close()
				return ErrArchive
			}
			leaf, err := x509.ParseCertificate(block.Bytes)
			if err != nil || !authenticatesArchivedCertificate(archivedAuthority, ca, leaf) || leaf.Subject.CommonName != nodeID {
				rows.Close()
				return ErrArchive
			}
			id, idErr := pki.NodeID(leaf)
			expires, parseErr := time.Parse(time.RFC3339Nano, expiry)
			oldBlock, _ := pem.Decode([]byte(currentPEM))
			if oldBlock == nil {
				rows.Close()
				return ErrArchive
			}
			old, oldErr := x509.ParseCertificate(oldBlock.Bytes)
			newPublic, newErr := x509.MarshalPKIXPublicKey(leaf.PublicKey)
			if oldErr != nil {
				rows.Close()
				return ErrArchive
			}
			oldPublic, oldErr := x509.MarshalPKIXPublicKey(old.PublicKey)
			if idErr != nil || id != nodeID || parseErr != nil || !expires.Equal(leaf.NotAfter) || newErr != nil || oldErr != nil || !bytes.Equal(newPublic, oldPublic) {
				rows.Close()
				return ErrArchive
			}
			digest := sha256.Sum256(block.Bytes)
			if fingerprint != hex.EncodeToString(digest[:]) {
				rows.Close()
				return ErrArchive
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return ErrArchive
		}
		rows.Close()
	}
	if err := access.ValidatePendingTraffic(ctx, database, configurationVault); err != nil {
		return ErrArchive
	}
	if err := userledger.ValidateTrafficMaterial(ctx, database); err != nil {
		return ErrArchive
	}
	if err := nodeledger.ValidateAcceptedMessageMaterial(ctx, database); err != nil {
		return ErrArchive
	}
	if err := nodeledger.ValidateEnrollmentReceiptMaterial(ctx, database); err != nil {
		return ErrArchive
	}

	if err := access.ValidateTrafficAccountingCompletion(ctx, database); err != nil {
		return ErrArchive
	}
	if err := access.ValidateRetentionMaterial(ctx, database); err != nil {
		return ErrArchive
	}
	if err := access.ValidateSpaceMaintenanceMaterial(ctx, database); err != nil {
		return ErrArchive
	}
	if err := validateCoreUpdates(ctx, database); err != nil {
		return err
	}
	if err := validateAgentUpdates(ctx, database); err != nil {
		return err
	}
	return nil
}

func validateMigrations(ctx context.Context, database *sql.DB) error {
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return ErrArchive
	}
	sort.Strings(names)
	rows, err := database.QueryContext(ctx, "SELECT version,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil || index >= len(names) || version != names[index] {
			return ErrArchive
		}
		data, err := fs.ReadFile(migrations.Files, version)
		if err != nil {
			return ErrArchive
		}
		digest := sha256.Sum256(data)
		if checksum != hex.EncodeToString(digest[:]) {
			return ErrArchive
		}
		index++
	}
	if err := rows.Err(); err != nil || index == 0 {
		return ErrArchive
	}
	return nil
}

// stagedTLS points the web TLS files at an unpacked archive. client_ca is
// optional, so it is expected only when the configuration names one.
func stagedTLS(directory string, clientCA bool) config.TLS {
	staged := config.TLS{Certificate: filepath.Join(directory, "tls", "server.crt"), Key: filepath.Join(directory, "tls", "server.key")}
	if clientCA {
		staged.ClientCA = filepath.Join(directory, "tls", "client-ca.crt")
	}
	return staged
}
