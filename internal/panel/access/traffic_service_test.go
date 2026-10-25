package access

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

// serviceWithDeletedIdentity builds the service of the shared fixture with a real
// user and device whose credential is exactly the one the applied revision
// contains, and deletes either of them before any counter arrives.
func serviceWithDeletedIdentity(t *testing.T, deleteDevice bool) (*Service, *user.Repository, user.User, user.Device, string) {
	t.Helper()
	r, s, record := fixture(t)
	ctx := context.Background()
	key, err := vault.Open(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: r.Database, Vault: key}
	u, err := users.Save(ctx, "", user.Input{Username: "metered_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Laptop", Protocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	state := s.Snapshots.(snapshots).state
	state.Credentials[0].ID = device.Credentials[0].ID
	state.Credentials[0].UserID = u.ID
	state.Credentials[0].DeviceID = device.ID
	state.Inbounds[0].CredentialIDs = []string{device.Credentials[0].ID}
	s.Snapshots = snapshots{state: state}
	s.Users = users
	if deleteDevice {
		if err := users.DeleteDevice(ctx, device.ID); err != nil {
			t.Fatal(err)
		}
	} else if err := users.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	return s, users, u, device, record.NodeID
}

func submitTraffic(t *testing.T, s *Service, nodeID, batchID string, counter traffic.Counter) error {
	t.Helper()
	batch := traffic.Batch{ID: batchID, NodeID: nodeID, Counters: []traffic.Counter{counter}}
	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordTraffic(context.Background(), nodeID, &pb.TrafficBatch{BatchJson: data}, true)
	return err
}

// A device deleted before its last counters arrived must not block the node: the
// same batch is accepted on every retry, charged to the surviving user once.
func TestDeletedDeviceCounterUploadsOnceAndIsIdempotent(t *testing.T) {
	s, users, u, device, nodeID := serviceWithDeletedIdentity(t, true)
	counter := traffic.Counter{CoreEpoch: "epoch-deleted-device", Revision: 1, CredentialID: device.Credentials[0].ID, UserID: u.ID, DeviceID: device.ID, Protocol: "vless", UploadBytes: 100, DownloadBytes: 900, ObservedAt: s.Now().Add(-time.Minute)}
	for attempt := 0; attempt < 3; attempt++ {
		if err := submitTraffic(t, s, nodeID, "batch-deleted-device", counter); err != nil {
			t.Fatalf("counter of a deleted device rejected on attempt %d: %v", attempt, err)
		}
	}
	updated, err := users.Get(context.Background(), u.ID)
	if err != nil || updated.UsedTraffic != 1000 {
		t.Fatalf("surviving user usage wrong: %+v %v", updated, err)
	}
	var samples int
	if err := s.Repository.Database.QueryRow("SELECT count(*) FROM account_traffic_samples").Scan(&samples); err != nil || samples != 1 {
		t.Fatalf("access-side samples counted wrongly: %d %v", samples, err)
	}
}

// The same holds for a deleted user, who no longer has a row to charge.
func TestDeletedUserCounterUploadsWithoutResurrectingTheUser(t *testing.T) {
	s, users, u, device, nodeID := serviceWithDeletedIdentity(t, false)
	counter := traffic.Counter{CoreEpoch: "epoch-deleted-user", Revision: 1, CredentialID: device.Credentials[0].ID, UserID: u.ID, DeviceID: device.ID, Protocol: "vless", UploadBytes: 100, DownloadBytes: 900, ObservedAt: s.Now().Add(-time.Minute)}
	for attempt := 0; attempt < 3; attempt++ {
		if err := submitTraffic(t, s, nodeID, "batch-deleted-user", counter); err != nil {
			t.Fatalf("counter of a deleted user rejected on attempt %d: %v", attempt, err)
		}
	}
	if _, err := users.Get(context.Background(), u.ID); err == nil {
		t.Fatal("deleted user was restored by its counters")
	}
	var cursors int
	if err := s.Repository.Database.QueryRow("SELECT count(*) FROM user_traffic_cursors").Scan(&cursors); err != nil || cursors != 1 {
		t.Fatalf("deleted identity cursor missing: %d %v", cursors, err)
	}
}
