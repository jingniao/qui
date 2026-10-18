package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func sourceFixture(t *testing.T) (Source, string, string) {
	t.Helper()
	root := t.TempDir()
	configuration := config.DefaultPanel()
	configuration.Database = filepath.Join(root, "panel.db")
	configuration.NodeIdentityDirectory = filepath.Join(root, "identity")
	database, err := db.Open(context.Background(), configuration.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	authority, err := pki.LoadOrCreate(configuration.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.DevelopmentServer(); err != nil {
		t.Fatal(err)
	}
	key, err := vault.Open(filepath.Join(configuration.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(context.Background(), "Backup node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: database, Vault: key}
	u, err := users.Save(context.Background(), "", user.Input{Username: "backup_user", DisplayName: "Backup user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(context.Background(), u.ID, user.DeviceInput{Name: "Laptop", Protocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := device.Credentials[0]
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: u.ID, Enabled: true}},
		Credentials: []desiredstate.Credential{{ID: credential.ID, UserID: u.ID, DeviceID: device.ID, Protocol: "http", Username: credential.Username, Secret: credential.Secret}},
		Inbounds:    []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: 1080, Enabled: true, CredentialIDs: []string{credential.ID}, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
	if _, err := (&deployment.Repository{Database: database, Vault: key}).Create(context.Background(), state, "owner", "Preserved access"); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(root, "rulesets", "company")
	if err := os.MkdirAll(rules, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "services.json"), []byte(`{"domains":["company.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "empty.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	return Source{Database: database, Config: configuration, ConfigurationVault: key}, filepath.Join(root, "backups"), credential.Secret
}

func archivedFiles(t *testing.T, archivePath string) map[string][]byte {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	sealed, err := newEncryptedReader(context.Background(), file, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := gzip.NewReader(sealed)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	result := map[string][]byte{}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(archive)
		if err != nil {
			t.Fatal(err)
		}
		result[header.Name] = data
	}
	return result
}

func assertNoStage(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("plaintext staging directory left behind", entry.Name())
		}
	}
}

func TestPortableSnapshotIncludesWALDataOriginalKeysRulesAndEncryptedCredentials(t *testing.T) {
	source, directory, secret := sourceFixture(t)
	wal, err := os.Stat(source.Config.Database + "-wal")
	if err != nil || wal.Size() == 0 {
		t.Fatal("fixture did not contain live WAL data", err)
	}
	result, err := source.Create(context.Background(), directory, "backup_fixture_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(directory, result.Manifest.ID+".pcb")
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if result.Bytes != int64(len(data)) || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("archive size or checksum does not match real file")
	}
	for _, marker := range [][]byte{[]byte(secret), []byte("company.example"), []byte("BEGIN PRIVATE KEY"), []byte("SQLite format 3")} {
		if bytes.Contains(data, marker) {
			t.Fatal("portable archive exposes plaintext backup contents")
		}
	}
	files := archivedFiles(t, archivePath)
	for _, name := range []string{"database/panel.db", "configuration/panel.yaml", "identity/ca.crt", "identity/ca.key", "identity/configuration.key", "identity/server.crt", "identity/server.key", "rulesets/company/services.json", "rulesets/company/empty.txt"} {
		if _, exists := files[name]; !exists {
			t.Fatal("required backup file missing", name)
		}
	}
	staging := t.TempDir()
	databasePath := filepath.Join(staging, "snapshot.db")
	if err := os.WriteFile(databasePath, files["database/panel.db"], 0600); err != nil {
		t.Fatal(err)
	}
	database, err := readonlyDatabase(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var users, devices, revisions int
	database.QueryRow("SELECT count(*) FROM users").Scan(&users)
	database.QueryRow("SELECT count(*) FROM devices").Scan(&devices)
	database.QueryRow("SELECT count(*) FROM config_revisions").Scan(&revisions)
	if users != 1 || devices != 1 || revisions != 1 {
		t.Fatal("WAL-backed committed business data lost", users, devices, revisions)
	}
	key, err := vault.FromKey(files["identity/configuration.key"])
	if err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err := database.QueryRow("SELECT encrypted_secret FROM credentials LIMIT 1").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	opened, err := key.Open(encrypted)
	var preserved user.Secret
	if err != nil || json.Unmarshal(opened, &preserved) != nil || preserved.Value != secret {
		t.Fatal("original credential key was not preserved", err)
	}
	assertNoStage(t, directory)
	if _, err := Inspect(context.Background(), directory, archivePath, "different-backup-password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	assertNoStage(t, directory)
	if _, err := source.Create(context.Background(), directory, result.Manifest.ID, "owner", testPassword); err == nil {
		t.Fatal("existing portable backup overwritten")
	}
	current, err := os.ReadFile(archivePath)
	if err != nil || !bytes.Equal(current, data) {
		t.Fatal("duplicate ID altered existing backup", err)
	}
	assertNoStage(t, directory)
}

func TestMissingReplacedKeysAndInvalidEncryptedRowsCannotPublishBackup(t *testing.T) {
	for _, fault := range []string{"missing-key", "changed-key", "invalid-row", "bad-ca", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			keyPath := filepath.Join(source.Config.NodeIdentityDirectory, "configuration.key")
			ctx := context.Background()
			switch fault {
			case "missing-key":
				if err := os.Remove(keyPath); err != nil {
					t.Fatal(err)
				}
			case "changed-key":
				if err := os.WriteFile(keyPath, bytes.Repeat([]byte{7}, 32), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-row":
				if _, err := source.Database.Exec("UPDATE credentials SET encrypted_secret=?", []byte("broken")); err != nil {
					t.Fatal(err)
				}
			case "bad-ca":
				if err := os.WriteFile(filepath.Join(source.Config.NodeIdentityDirectory, "ca.key"), []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := source.Create(ctx, directory, "failed_backup_001", "owner", testPassword); err == nil {
				t.Fatal("invalid source published backup")
			}
			if _, err := os.Stat(filepath.Join(directory, "failed_backup_001.pcb")); !os.IsNotExist(err) {
				t.Fatal("failed creation left a final archive", err)
			}
			assertNoStage(t, directory)
			if fault == "missing-key" {
				if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
					t.Fatal("missing original key was regenerated")
				}
			}
		})
	}
}

func TestManifestRejectsTraversalDuplicateMissingFilesAndOversizedMaterial(t *testing.T) {
	for _, fault := range []string{"traversal", "duplicate", "missing", "oversized-key", "unknown-field"} {
		t.Run(fault, func(t *testing.T) {
			files := []File{}
			for _, name := range []string{"database/panel.db", "configuration/panel.yaml", "identity/ca.crt", "identity/ca.key", "identity/configuration.key"} {
				files = append(files, File{Name: name, Bytes: 1, SHA256: hex.EncodeToString(make([]byte, 32))})
			}
			manifest := Manifest{Version: 1, ID: "valid_backup_001", CreatedAt: time.Now().UTC(), CreatedBy: "owner", Files: files}
			switch fault {
			case "traversal":
				manifest.Files[0].Name = "rulesets/../../outside.key"
			case "duplicate":
				manifest.Files = append(manifest.Files, manifest.Files[0])
			case "missing":
				manifest.Files = manifest.Files[1:]
			case "oversized-key":
				manifest.Files[4].Bytes = 33
			}
			data, _ := json.Marshal(manifest)
			if fault == "unknown-field" {
				data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"command":"shell"`), 1)
			}
			if _, err := decodeManifest(data); err == nil {
				t.Fatal("invalid manifest accepted", fault)
			}
		})
	}
}
