package deployment

import (
	"bytes"
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func TestEncryptedSnapshotPendingPersistenceAndIdempotentResult(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "panel.db")
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Node A", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Credentials: []desiredstate.Credential{{ID: "device-credential", Protocol: "socks5", Username: "device", Secret: "private-device-password"}}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Enabled: true, Listen: "127.0.0.1", Port: 1080, DefaultOutbound: "direct", CredentialIDs: []string{"device-credential"}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	repository := &Repository{Database: database, Vault: configurationVault}
	revision, err := repository.Create(ctx, state, "owner", "Device access")
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := database.QueryRow("SELECT encrypted_state FROM config_revisions WHERE id=?", revision.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(state.Credentials[0].Secret)) {
		t.Fatal("snapshot stored plaintext credential")
	}
	if err := repository.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Deploy(ctx, revision.ID); err != nil {
		t.Fatal("duplicate deploy failed", err)
	}
	database.Close()
	database, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	repository.Database = database
	pending, err := repository.Pending(ctx, n.ID)
	if err != nil || pending == nil {
		t.Fatal("pending deployment lost on reopen", err)
	}
	decoded, err := desiredstate.Decode(pending.DesiredStateJson)
	if err != nil || decoded.Credentials[0].Secret != state.Credentials[0].Secret {
		t.Fatal("snapshot did not decrypt", err)
	}
	wrong := &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}
	if err := repository.Result(ctx, "different-node", wrong); err == nil {
		t.Fatal("accepted another node's result")
	}
	if err := repository.Result(ctx, n.ID, wrong); err != nil {
		t.Fatal(err)
	}
	if err := repository.Result(ctx, n.ID, wrong); err != nil {
		t.Fatal("duplicate result failed", err)
	}
	if command, err := repository.Pending(ctx, n.ID); err != nil || command != nil {
		t.Fatal("successful deployment remained pending")
	}
	if err := repository.Result(ctx, n.ID, &pb.ApplyResult{Revision: revision.ID, Code: "failed"}); err == nil {
		t.Fatal("conflicting result accepted")
	}
}
