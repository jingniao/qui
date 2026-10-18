package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

// TestGeneratePreRefactorFixture is a generator, not a test. It must be run
// from a worktree checked out at the pre-refactor baseline commit de6a681 (with
// the original bytes of migrations/0004_deployment.sql and
// migrations/0036_agent_updates.sql copied in) so that the archive it produces
// carries the baseline schema_migrations checksums.
//
// It boots the baseline server, creates business data through the real HTTP
// API plus one raw SQL row in a receipt-archive table and one budget row (with
// its immutable version), then creates an encrypted backup through the backups
// API and copies it to $PREREFACTOR_FIXTURE_OUT. Without that variable it
// skips.
func TestGeneratePreRefactorFixture(t *testing.T) {
	output := os.Getenv("PREREFACTOR_FIXTURE_OUT")
	if output == "" {
		t.Skip("PREREFACTOR_FIXTURE_OUT not set; generator skipped")
	}
	ctx := context.Background()
	app := testServer(t)
	owner := budgetOwner(t, app)

	groupResponse := owner.call("POST", "/api/v1/node-groups", map[string]any{
		"name":                "Fixture Group",
		"purpose":             "subscription",
		"enabled":             true,
		"subscriptionVisible": true,
		"nodeIds":             []string{},
	})
	if groupResponse.Code != 201 {
		t.Fatalf("create node group: %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	var group node.Group
	if err := json.Unmarshal(groupResponse.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}

	userResponse := owner.call("POST", "/api/v1/users", user.Input{
		Username:         "fixture_user",
		DisplayName:      "Fixture User",
		Status:           "active",
		MaxDevices:       1,
		MaxConnections:   8,
		AllowedProtocols: []string{"vless"},
		NodeGroupIDs:     []string{group.ID},
	})
	if userResponse.Code != 201 {
		t.Fatalf("create user: %d %s", userResponse.Code, userResponse.Body.String())
	}
	var createdUser user.User
	if err := json.Unmarshal(userResponse.Body.Bytes(), &createdUser); err != nil {
		t.Fatal(err)
	}

	policyResponse := owner.call("POST", "/api/v1/policies", policy.Input{
		Name:     "Fixture block",
		Priority: 10,
		Enabled:  true,
		Match:    desiredstate.Match{Domains: []string{"blocked.example"}, Network: "tcp"},
		Action:   desiredstate.Action{Type: "Block"},
	})
	if policyResponse.Code != 201 {
		t.Fatalf("create policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	nodes := &node.Repository{Database: app.Database}
	createdNode, _, _, err := nodes.CreateEnrollment(ctx, "Fixture node", time.Hour)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	if err := insertAccessReceiptArchive(ctx, app, createdNode.ID, now); err != nil {
		t.Fatalf("insert access receipt archive: %v", err)
	}
	if err := insertBudget(ctx, app, createdUser.ID, createdNode.ID, now); err != nil {
		t.Fatalf("insert budget: %v", err)
	}

	const password = "fixture-backup-password"
	backupResponse := owner.call("POST", "/api/v1/backups", map[string]any{"password": password})
	if backupResponse.Code != 201 {
		t.Fatalf("create backup: %d %s", backupResponse.Code, backupResponse.Body.String())
	}
	var item system.Backup
	if err := json.Unmarshal(backupResponse.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(filepath.Dir(app.Config.Database), "backups", item.ID+".pcb")
	in, err := os.Open(source)
	if err != nil {
		t.Fatalf("open produced backup: %v", err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(output)
	if err != nil {
		t.Fatalf("create fixture output: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatalf("write fixture output: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote pre-refactor fixture %s (%d bytes)", output, item.FileBytes)
}

type fixtureArchivedReceipt struct {
	ID         string `json:"id"`
	Digest     string `json:"digest"`
	ReceivedAt string `json:"receivedAt"`
}

type fixtureReceiptArchivePage struct {
	Version  int                      `json:"version"`
	NodeID   string                   `json:"nodeId"`
	Receipts []fixtureArchivedReceipt `json:"receipts"`
}

// insertAccessReceiptArchive writes a row that the baseline backup inspection
// accepts: the payload is the vault-sealed gzip of the archive page JSON and
// the digest column is the SHA-256 of that plaintext, exactly as the access
// module stores real pages.
func insertAccessReceiptArchive(ctx context.Context, app *Server, nodeID, created string) error {
	key, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		return err
	}
	material := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	at := time.Now().UTC()
	page := fixtureReceiptArchivePage{
		Version: 1,
		NodeID:  nodeID,
		Receipts: []fixtureArchivedReceipt{
			{ID: "fixture-batch-0001", Digest: material("fixture-batch-0001"), ReceivedAt: at.Add(-2 * time.Minute).Format(time.RFC3339Nano)},
			{ID: "fixture-batch-0002", Digest: material("fixture-batch-0002"), ReceivedAt: at.Add(-1 * time.Minute).Format(time.RFC3339Nano)},
		},
	}
	plain, err := json.Marshal(page)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(plain)
	digest := hex.EncodeToString(sum[:])
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(plain); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	sealed, err := key.Seal(compressed.Bytes())
	if err != nil {
		return err
	}
	_, err = app.Database.ExecContext(ctx,
		"INSERT INTO access_receipt_archives(node_id,first_id,last_id,records,digest,payload,created_at) VALUES(?,?,?,?,?,?,?)",
		nodeID, page.Receipts[0].ID, page.Receipts[len(page.Receipts)-1].ID, len(page.Receipts), digest, sealed, created)
	return err
}

// insertBudget writes a reservation and its single immutable version. Budget
// inspection requires the version series to be exactly 1..sequence.
func insertBudget(ctx context.Context, app *Server, userID, nodeID, created string) error {
	if _, err := app.Database.ExecContext(ctx,
		"INSERT INTO user_node_budgets(id,user_id,node_id,sequence,ceiling_bytes,reported_spent,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)",
		"fixture-budget", userID, nodeID, 1, 1048576, 0, created, created); err != nil {
		return err
	}
	_, err := app.Database.ExecContext(ctx,
		"INSERT INTO user_node_budget_versions(budget_id,sequence,ceiling_bytes) VALUES(?,?,?)",
		"fixture-budget", 1, 1048576)
	return err
}
