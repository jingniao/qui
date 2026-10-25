package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// trafficFixture creates a user with one device whose credential is exactly what
// the published revision contains, which is what a node reports counters for.
func trafficFixture(t *testing.T) (*Repository, User, Device, desiredstate.State) {
	t.Helper()
	ctx := context.Background()
	repository := testRepository(t)
	u, err := repository.Save(ctx, "", Input{Username: "traffic_user", DisplayName: "Traffic", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Laptop", Protocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "node-a", Credentials: []desiredstate.Credential{{ID: device.Credentials[0].ID, UserID: u.ID, DeviceID: device.ID, Protocol: "vless", Secret: "unused-test-secret"}}}
	return repository, u, device, state
}

func trafficBatch(userID, deviceID, credentialID string, observed time.Time) traffic.Batch {
	return traffic.Batch{ID: "batch-one", NodeID: "node-a", Counters: []traffic.Counter{{CoreEpoch: "epoch-one", Revision: 1, CredentialID: credentialID, UserID: userID, DeviceID: deviceID, Protocol: "vless", UploadBytes: 100, DownloadBytes: 900, ObservedAt: observed}}}
}

func statesOf(state desiredstate.State) map[int64]desiredstate.State {
	return map[int64]desiredstate.State{state.Revision: state}
}

func countRows(t *testing.T, repository *Repository, query string, args ...any) int {
	t.Helper()
	var total int
	if err := repository.Database.QueryRow(query, args...).Scan(&total); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return total
}

func cursorOf(t *testing.T, repository *Repository, credentialID string) (string, string, int64, int64) {
	t.Helper()
	var userID, deviceID string
	var upload, download int64
	if err := repository.Database.QueryRow("SELECT user_id,device_id,upload_bytes,download_bytes FROM user_traffic_cursors WHERE credential_id=?", credentialID).Scan(&userID, &deviceID, &upload, &download); err != nil {
		t.Fatalf("traffic cursor for a removed identity: %v", err)
	}
	return userID, deviceID, upload, download
}

// A deleted user's node keeps sending the counters it sampled under the revision
// that contained the credential. They must be accepted exactly once (so the
// agent can ACK them) while the deleted rows stay deleted.
func TestDeletedUserCountersAreAcceptedOnceAndKeptIdempotent(t *testing.T) {
	ctx := context.Background()
	repository, u, device, state := trafficFixture(t)
	batch := trafficBatch(u.ID, device.ID, device.Credentials[0].ID, time.Now().UTC().Add(-time.Minute))
	if err := repository.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	var applied []string
	repository.TrafficApplied = func(_ context.Context, userIDs []string) { applied = append(applied, userIDs...) }
	for attempt := 0; attempt < 3; attempt++ {
		if err := repository.ApplyTrafficWithStates(ctx, batch, statesOf(state)); err != nil {
			t.Fatalf("deleted user's counter %d rejected: %v", attempt, err)
		}
	}
	userID, deviceID, upload, download := cursorOf(t, repository, device.Credentials[0].ID)
	if userID != u.ID || deviceID != device.ID || upload != 100 || download != 900 {
		t.Fatalf("deleted identity cursor not advanced: %s %s %d %d", userID, deviceID, upload, download)
	}
	if len(applied) != 0 {
		t.Fatalf("quota evaluated for a deleted user: %v", applied)
	}
	for _, check := range []struct {
		query string
		args  []any
		want  int
	}{
		{"SELECT count(*) FROM users", nil, 0},
		{"SELECT count(*) FROM devices", nil, 0},
		{"SELECT count(*) FROM credentials", nil, 0},
		{"SELECT count(*) FROM credential_tombstones WHERE credential_id=?", []any{device.Credentials[0].ID}, 1},
	} {
		if got := countRows(t, repository, check.query, check.args...); got != check.want {
			t.Fatalf("%s = %d, want %d", check.query, got, check.want)
		}
	}
}

// Deleting one device leaves its user in place: the user keeps accumulating the
// bytes of the deleted device exactly once, and the device row does not return.
func TestDeletedDeviceChargesItsSurvivingUserOnce(t *testing.T) {
	ctx := context.Background()
	repository, u, device, state := trafficFixture(t)
	batch := trafficBatch(u.ID, device.ID, device.Credentials[0].ID, time.Now().UTC().Add(-time.Minute))
	if err := repository.DeleteDevice(ctx, device.ID); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := repository.ApplyTrafficWithStates(ctx, batch, statesOf(state)); err != nil {
			t.Fatalf("deleted device's counter %d rejected: %v", attempt, err)
		}
	}
	updated, err := repository.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UsedTraffic != 1000 || updated.DevicesCount != 0 {
		t.Fatalf("surviving user charged wrongly: used=%d devices=%d", updated.UsedTraffic, updated.DevicesCount)
	}
	if got := countRows(t, repository, "SELECT count(*) FROM devices WHERE id=?", device.ID); got != 0 {
		t.Fatalf("deleted device restored: %d", got)
	}
	cursorOf(t, repository, device.Credentials[0].ID)
}

// Without a tombstone the identity is genuinely unknown, no matter what the
// revision says, and the cursor must not advance.
func TestUnknownIdentityWithoutATombstoneStaysConflict(t *testing.T) {
	ctx := context.Background()
	repository, u, device, state := trafficFixture(t)
	state.Credentials[0].DeviceID = "unknown-device"
	batch := trafficBatch(u.ID, "unknown-device", device.Credentials[0].ID, time.Now().UTC().Add(-time.Minute))
	if err := repository.ApplyTrafficWithStates(ctx, batch, statesOf(state)); !errors.Is(err, ErrTrafficIdentity) {
		t.Fatalf("unknown identity accepted: %v", err)
	}
	updated, err := repository.Get(ctx, u.ID)
	if err != nil || updated.UsedTraffic != 0 {
		t.Fatalf("conflicting counter was charged: %d %v", updated.UsedTraffic, err)
	}
	if got := countRows(t, repository, "SELECT count(*) FROM user_traffic_cursors"); got != 0 {
		t.Fatalf("conflicting counter advanced a cursor: %d", got)
	}
}

// A tombstone alone is not enough: the counter must name a revision that still
// contained the identity, otherwise the deletion does not explain the row.
func TestTombstoneWithoutRevisionEvidenceStaysConflict(t *testing.T) {
	ctx := context.Background()
	repository, u, device, state := trafficFixture(t)
	batch := trafficBatch(u.ID, device.ID, device.Credentials[0].ID, time.Now().UTC().Add(-time.Minute))
	if err := repository.DeleteDevice(ctx, device.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyTrafficWithStates(ctx, batch, nil); !errors.Is(err, ErrTrafficIdentity) {
		t.Fatalf("missing revision evidence accepted: %v", err)
	}
	other := state
	other.Revision = 2
	if err := repository.ApplyTrafficWithStates(ctx, batch, statesOf(other)); !errors.Is(err, ErrTrafficIdentity) {
		t.Fatalf("unrelated revision accepted: %v", err)
	}
	withoutCredential := state
	withoutCredential.Credentials = nil
	if err := repository.ApplyTrafficWithStates(ctx, batch, statesOf(withoutCredential)); !errors.Is(err, ErrTrafficIdentity) {
		t.Fatalf("revision without the credential accepted: %v", err)
	}
	if got := countRows(t, repository, "SELECT count(*) FROM user_traffic_cursors"); got != 0 {
		t.Fatalf("rejected counters advanced a cursor: %d", got)
	}
	if err := repository.ApplyTrafficWithStates(ctx, batch, statesOf(state)); err != nil {
		t.Fatalf("the counter's own revision was rejected: %v", err)
	}
}

// A cursor that already belongs to another identity inside the same epoch stays a
// conflict even when the reported identity is tombstoned.
func TestCursorOwnerMismatchStaysConflict(t *testing.T) {
	ctx := context.Background()
	repository, u, device, state := trafficFixture(t)
	batch := trafficBatch(u.ID, device.ID, device.Credentials[0].ID, time.Now().UTC().Add(-time.Minute))
	if err := repository.ApplyTrafficWithStates(ctx, batch, statesOf(state)); err != nil {
		t.Fatal(err)
	}
	moved := batch
	moved.Counters = append([]traffic.Counter(nil), batch.Counters...)
	moved.Counters[0].DeviceID = "other-device"
	if err := repository.ApplyTrafficWithStates(ctx, moved, statesOf(state)); !errors.Is(err, ErrTrafficIdentity) {
		t.Fatalf("cursor owner mismatch accepted: %v", err)
	}
}
