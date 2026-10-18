package spool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/traffic"
)

// olderSchemaSQL is the spool schema an agent at b763421 created on its first
// start. It carries the two live queues the reduced spool keeps and the
// final-usage, runtime-source, budget, replay-ack and database-installation
// tables it no longer uses. Records queued by that agent must still upload.
const olderSchemaSQL = `
CREATE TABLE spool_metadata(id INTEGER PRIMARY KEY CHECK(id=1),schema_version INTEGER NOT NULL,node_id TEXT NOT NULL,dropped_allowed INTEGER NOT NULL DEFAULT 0);
CREATE TABLE batches(id TEXT PRIMARY KEY,blocked INTEGER NOT NULL CHECK(blocked IN (0,1)),records INTEGER NOT NULL,payload BLOB NOT NULL);
CREATE INDEX batch_eviction ON batches(blocked);
CREATE TABLE traffic_batches(id TEXT PRIMARY KEY,payload BLOB NOT NULL);
CREATE TABLE traffic_cursors(epoch TEXT NOT NULL,credential_id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(epoch,credential_id));
CREATE TABLE budget_state(id TEXT PRIMARY KEY,user_id TEXT NOT NULL,payload BLOB NOT NULL);
CREATE UNIQUE INDEX one_user_budget ON budget_state(user_id);
CREATE TABLE final_usage_queue(epoch TEXT PRIMARY KEY,payload BLOB NOT NULL);
CREATE TABLE runtime_source_metadata(id INTEGER PRIMARY KEY CHECK(id=1),payload BLOB NOT NULL);
CREATE TABLE runtime_sources(epoch TEXT PRIMARY KEY,sequence INTEGER NOT NULL UNIQUE,payload BLOB NOT NULL,acknowledged INTEGER NOT NULL DEFAULT 0 CHECK(acknowledged IN (0,1)),closed_digest TEXT NOT NULL DEFAULT '');
CREATE TABLE budget_retirements(id TEXT PRIMARY KEY,budget_id TEXT NOT NULL,sequence INTEGER NOT NULL CHECK(sequence>=1),payload BLOB NOT NULL,UNIQUE(budget_id,sequence));
CREATE TABLE replay_ack_state(kind TEXT PRIMARY KEY,payload BLOB NOT NULL);
ALTER TABLE spool_metadata ADD COLUMN collector_loss_enabled INTEGER NOT NULL DEFAULT 0 CHECK(collector_loss_enabled IN (0,1));
ALTER TABLE spool_metadata ADD COLUMN replay_ack_enabled INTEGER NOT NULL DEFAULT 0 CHECK(replay_ack_enabled IN (0,1));
CREATE TABLE collector_loss_state(id INTEGER PRIMARY KEY CHECK(id=1),payload BLOB NOT NULL);
`

type olderSpool struct {
	directory string
	witness   string
	allowed   string
	blocked   string
	traffic   string
}

// bindOlderStore publishes the authenticated v1 checkpoint the older agent
// wrote: the head is sealed with the spool key and mirrored in the witness file
// outside the queue directory. The digest is the shared v1 digest, which
// TestDroppingOlderSpoolTablesKeepsTheCheckpointDigest proves is unchanged.
func bindOlderStore(t *testing.T, fixture olderSpool) {
	t.Helper()
	v, err := vault.Open(filepath.Join(fixture.directory, "spool.key"))
	if err != nil {
		t.Fatal(err)
	}
	database := rawSpoolDatabase(t, filepath.Join(fixture.directory, "queue.db"))
	defer database.Close()
	tx, err := database.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	digest, budgeted, err := criticalDigest(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	storeID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.Seal(mustJSON(t, storeHead{Version: 1, NodeID: "node", StoreID: storeID, Digest: digest, Budgeted: budgeted, Witness: fixture.witness}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE TABLE spool_store(id INTEGER PRIMARY KEY CHECK(id=1),payload BLOB NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO spool_store(id,payload) VALUES(1,?)", sealed); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.witness), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.witness, sealed, 0600); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rawSpoolDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	return database
}

func queueAccessBatch(t *testing.T, v *vault.Vault, database *sql.DB, domain string, blocked bool) string {
	t.Helper()
	batchID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	batch := access.Batch{ID: batchID, NodeID: "node", Records: []access.Rollup{row(domain, blocked)}}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.Seal(data)
	if err != nil {
		t.Fatal(err)
	}
	priority := 0
	if blocked {
		priority = 1
	}
	if _, err := database.Exec("INSERT INTO batches(id,blocked,records,payload) VALUES(?,?,?,?)", batchID, priority, len(batch.Records), sealed); err != nil {
		t.Fatal(err)
	}
	return batchID
}

func queueTrafficBatch(t *testing.T, v *vault.Vault, database *sql.DB) string {
	t.Helper()
	batchID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	counter := traffic.Counter{CoreEpoch: "older-epoch", Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", UploadBytes: 4096, DownloadBytes: 8192, ObservedAt: time.Now().UTC()}
	batch := traffic.Batch{ID: batchID, NodeID: "node", Counters: []traffic.Counter{counter}}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.Seal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO traffic_batches(id,payload) VALUES(?,?)", batchID, sealed); err != nil {
		t.Fatal(err)
	}
	cursorData, err := json.Marshal(cursor{Upload: counter.UploadBytes, Download: counter.DownloadBytes})
	if err != nil {
		t.Fatal(err)
	}
	sealedCursor, err := v.Seal(cursorData)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO traffic_cursors(epoch,credential_id,payload) VALUES(?,?,?)", counter.CoreEpoch, counter.CredentialID, sealedCursor); err != nil {
		t.Fatal(err)
	}
	return batchID
}

// olderSpoolFixture builds the database an agent at b763421 left behind with
// records still waiting for the central panel.
func olderSpoolFixture(t *testing.T) olderSpool {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "spool")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(filepath.Join(directory, "spool.key"))
	if err != nil {
		t.Fatal(err)
	}
	database := rawSpoolDatabase(t, filepath.Join(directory, "queue.db"))
	defer database.Close()
	if _, err := database.Exec(olderSchemaSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO spool_metadata(id,schema_version,node_id,collector_loss_enabled,replay_ack_enabled) VALUES(1,1,'node',1,1)"); err != nil {
		t.Fatal(err)
	}
	fixture := olderSpool{directory: directory, witness: filepath.Join(filepath.Dir(directory), "identity", "spool-store.json.enc")}
	fixture.allowed = queueAccessBatch(t, v, database, "allowed.example", false)
	fixture.blocked = queueAccessBatch(t, v, database, "blocked.example", true)
	fixture.traffic = queueTrafficBatch(t, v, database)
	// Residue of the deleted features. The reduced spool must ignore it.
	sealed, err := v.Seal([]byte(`{"ignored":true}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO final_usage_queue(epoch,payload) VALUES('older-epoch',?)",
		"INSERT INTO runtime_source_metadata(id,payload) VALUES(1,?)",
		"INSERT INTO runtime_sources(epoch,sequence,payload,acknowledged) VALUES('older-epoch',1,?,0)",
		"INSERT INTO budget_retirements(id,budget_id,sequence,payload) VALUES('older-retirement','older-budget',1,?)",
		"INSERT INTO replay_ack_state(kind,payload) VALUES('access',?)",
	} {
		if _, err := database.Exec(statement, sealed); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

// TestOlderSpoolFormatStillDrainsAfterUpgrade proves that records an older agent
// queued are still read, uploaded and deleted once acknowledged. The tables of
// the deleted features are ignored, not read, and never block the queues.
func TestOlderSpoolFormatStillDrainsAfterUpgrade(t *testing.T) {
	ctx := context.Background()
	fixture := olderSpoolFixture(t)
	s, err := Open(ctx, fixture.directory, "node", 0)
	if err != nil {
		t.Fatalf("older spool refused after upgrade: %v", err)
	}
	pending, err := s.Pending(ctx)
	if err != nil || pending == nil || pending.ID != fixture.allowed {
		s.Close()
		t.Fatalf("queued access record lost after upgrade: %+v %v", pending, err)
	}
	queued, err := s.PendingTraffic(ctx)
	if err != nil || queued == nil || queued.ID != fixture.traffic {
		s.Close()
		t.Fatalf("queued traffic record lost after upgrade: %+v %v", queued, err)
	}
	stats, err := s.Stats(ctx)
	// Two access batches and one traffic batch. The older Stats counted the
	// final-usage and runtime-source rows as well, so a larger count here would
	// mean the deleted features are still read.
	if err != nil || stats.Batches != 3 || stats.Rollups != 2 {
		s.Close()
		t.Fatalf("deleted-feature residue counted: %+v %v", stats, err)
	}
	if err := s.Ack(ctx, pending.ID); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.AckTraffic(ctx, queued.ID); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if batch, err := s.Pending(ctx); err != nil || batch == nil || batch.ID != fixture.blocked {
		s.Close()
		t.Fatalf("blocked record not retained after acknowledgement: %+v %v", batch, err)
	}
	if batch, err := s.PendingTraffic(ctx); err != nil || batch != nil {
		s.Close()
		t.Fatalf("acknowledged traffic record retained: %+v %v", batch, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// The drained store keeps working and still ignores the older tables.
	reopened, err := Open(ctx, fixture.directory, "node", 0)
	if err != nil {
		t.Fatalf("drained store refused: %v", err)
	}
	defer reopened.Close()
	var ignored int
	if err := reopened.database.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('final_usage_queue','runtime_source_metadata','runtime_sources','budget_retirements')").Scan(&ignored); err != nil || ignored != 4 {
		t.Fatalf("older tables are not preserved untouched: %d %v", ignored, err)
	}
	if err := reopened.Ack(ctx, fixture.blocked); err != nil {
		t.Fatal(err)
	}
	final, err := reopened.Stats(ctx)
	if err != nil || final.Batches != 0 || final.Bytes != 0 {
		t.Fatalf("acknowledged records retained: %+v %v", final, err)
	}
}

func spoolCheckpointDigest(t *testing.T, s *Spool) string {
	t.Helper()
	tx, err := s.database.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	digest, _, err := criticalDigest(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// TestDroppingOlderSpoolTablesKeepsTheCheckpointDigest pins the upgrade
// compatibility of the authenticated v1 checkpoint: the digest of a store that
// still carries the deleted tables equals the digest of the same store after
// they are dropped, because the tables were empty and only their names remain.
func TestDroppingOlderSpoolTablesKeepsTheCheckpointDigest(t *testing.T) {
	ctx := context.Background()
	fixture := olderSpoolFixture(t)
	s, err := Open(ctx, fixture.directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before := spoolCheckpointDigest(t, s)
	for _, table := range []string{"final_usage_queue", "runtime_source_metadata", "runtime_sources", "budget_retirements"} {
		if _, err := s.database.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if after := spoolCheckpointDigest(t, s); after != before {
		t.Fatalf("checkpoint digest changed when the deleted tables were dropped: %s != %s", before, after)
	}
}

// TestOlderBoundSpoolVerifiesAndDrainsAfterUpgrade covers the store the live
// agent left behind: an authenticated v1 checkpoint plus its witness file. The
// reduced spool must still verify it and upload the queued records.
func TestOlderBoundSpoolVerifiesAndDrainsAfterUpgrade(t *testing.T) {
	ctx := context.Background()
	fixture := olderSpoolFixture(t)
	bindOlderStore(t, fixture)
	s, err := OpenBound(ctx, fixture.directory, "node", 0, fixture.witness, nil)
	if err != nil {
		t.Fatalf("older bound spool refused after upgrade: %v", err)
	}
	defer s.Close()
	pending, err := s.Pending(ctx)
	if err != nil || pending == nil || pending.ID != fixture.allowed {
		t.Fatalf("queued access record lost after upgrade: %+v %v", pending, err)
	}
	queued, err := s.PendingTraffic(ctx)
	if err != nil || queued == nil || queued.ID != fixture.traffic {
		t.Fatalf("queued traffic record lost after upgrade: %+v %v", queued, err)
	}
	if err := s.Ack(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AckTraffic(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	if batch, err := s.Pending(ctx); err != nil || batch == nil || batch.ID != fixture.blocked {
		t.Fatalf("blocked record not retained after acknowledgement: %+v %v", batch, err)
	}
}

// TestSpoolCapacityKeepsBlockedAndTrafficBeforeAllowed checks the eviction order
// the reduced spool keeps: only authenticated allowed access records are
// sacrificed for capacity, oldest first.
func TestSpoolCapacityKeepsBlockedAndTrafficBeforeAllowed(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir(), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blocked, err := s.Enqueue(ctx, []access.Rollup{row("blocked.example", true)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTraffic(ctx, []traffic.Counter{sample("epoch", 500)}); err != nil {
		t.Fatal(err)
	}
	allowed, err := s.Enqueue(ctx, []access.Rollup{row("allowed.example", false)})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.limit = initial.Bytes
	next, err := s.Enqueue(ctx, []access.Rollup{row("evicted.example", false)})
	if err != nil || next.DroppedAllowedRollups != 1 {
		t.Fatalf("oldest allowed record not evicted first: %+v %v", next, err)
	}
	var count int
	if err := s.database.QueryRow("SELECT count(*) FROM batches WHERE id=?", blocked.BatchIDs[0]).Scan(&count); err != nil || count != 1 {
		t.Fatalf("blocked access record evicted: %d %v", count, err)
	}
	if err := s.database.QueryRow("SELECT count(*) FROM batches WHERE id=?", allowed.BatchIDs[0]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("oldest allowed record retained: %d %v", count, err)
	}
	if batch, err := s.PendingTraffic(ctx); err != nil || batch == nil {
		t.Fatalf("traffic record evicted for capacity: %+v %v", batch, err)
	}
	before, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, []access.Rollup{row("another-blocked.example", true)}); !errors.Is(err, ErrFull) {
		t.Fatalf("capacity overflow not reported while protected records remain: %v", err)
	}
	if after, err := s.Stats(ctx); err != nil || after != before {
		t.Fatalf("failed enqueue altered the protected records: %+v %v", after, err)
	}
}

// TestSpoolOfflineQueueUploadsAndDeletesAfterAck covers the offline queue,
// reconnect, upload and acknowledgement cycle for both queues.
func TestSpoolOfflineQueueUploadsAndDeletesAfterAck(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	s, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.Enqueue(ctx, []access.Rollup{row("offline.example", false), row("offline-blocked.example", true)})
	if err != nil || len(queued.BatchIDs) != 2 {
		t.Fatalf("offline access queue failed: %+v %v", queued, err)
	}
	if err := s.SaveTraffic(ctx, []traffic.Counter{sample("epoch", 42)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for {
		batch, err := s.Pending(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if batch == nil {
			break
		}
		if err := s.Ack(ctx, batch.ID); err != nil {
			t.Fatal(err)
		}
	}
	for {
		batch, err := s.PendingTraffic(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if batch == nil {
			break
		}
		if err := s.AckTraffic(ctx, batch.ID); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Batches != 0 || stats.Rollups != 0 || stats.Bytes != 0 {
		t.Fatalf("acknowledged records retained: %+v %v", stats, err)
	}
	if batch, err := s.Pending(ctx); err != nil || batch != nil {
		t.Fatalf("access queue not empty after acknowledgement: %+v %v", batch, err)
	}
	if batch, err := s.PendingTraffic(ctx); err != nil || batch != nil {
		t.Fatalf("traffic queue not empty after acknowledgement: %+v %v", batch, err)
	}
}
