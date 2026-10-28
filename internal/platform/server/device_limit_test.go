package server

import (
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/portal"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/devicegate"
	"proxycontrol/pkg/contracts/traffic"
	"strings"
	"testing"
	"time"
)

func TestDeviceLimitViewsResetPrivacyAndOldNodePartial(t *testing.T) {
	f := newQuotaFixture(t)
	u, c := f.addUser(t, "device_limit_user", "")
	revision := f.publish(t)
	now := time.Unix(1000, 0)
	o := f.app.deviceObserver
	o.Admission.Now = func() time.Time { return now }
	o.Now = func() time.Time { return now }
	input := user.Input{Username: u.Username, Status: "active", DeviceLimit: 1, AllowedProtocols: u.AllowedProtocols, NodeGroupIDs: u.NodeGroupIDs}
	if r := f.owner.call("PUT", "/api/v1/users/"+u.ID, input); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	s := traffic.ConnectionSnapshot{NodeID: f.nodeID, Revision: revision, CoreEpoch: "e", ObservedAt: now, Scope: traffic.ConnectionScope, ObservationContractVersion: 2, DeviceGateSupported: true, Connections: 3, Accounts: []traffic.ConnectionAccount{{CredentialID: c.ID, Connections: 3, AddressesMeasured: true, AddressKeys: []string{"198.51.100.17", "198.51.101.91", "2001:db8:17:1::/64"}}}}
	before := portalDatabaseSnapshot(t, f.app)
	if err := f.app.connectionStore.Record(f.nodeID, s); err != nil {
		t.Fatal(err)
	}
	o.Record(f.ctx, f.nodeID)
	command, err := o.Command(f.ctx, f.nodeID)
	if err != nil || command == nil {
		t.Fatal("missing gate", err)
	}
	gate, err := devicegate.Decode(command.RequestJson)
	if err != nil || len(gate.Entries) != 1 || len(gate.Entries[0].Keys) != 2 {
		t.Fatal("wrong gate command")
	}
	_, _ = o.Command(f.ctx, f.nodeID) // One user-access alert per hour, with no address fields.
	ownerView := f.owner.call("GET", "/api/v1/users/"+u.ID+"/devices-online", nil)
	var view user.DevicesOnline
	if ownerView.Code != 200 || json.Unmarshal(ownerView.Body.Bytes(), &view) != nil || view.OnlineDevices == nil || *view.OnlineDevices != 3 || view.Enforced || len(view.UnsupportedNodes) != 1 || view.UnsupportedNodes[0] != "Quota Node" {
		t.Fatal("old node not explicitly partial", ownerView.Body.String())
	}
	for _, private := range s.Accounts[0].AddressKeys {
		if strings.Contains(ownerView.Body.String(), private) {
			t.Fatal("Owner address not masked")
		}
	}
	if !strings.Contains(ownerView.Body.String(), "198.51.100.0/24") || !strings.Contains(ownerView.Body.String(), "2001:db8:17::/48") {
		t.Fatal("missing masks")
	}
	after := portalDatabaseSnapshot(t, f.app)
	// Observation and gate generation are read-only even though all tables are
	// inspected, including audit rows and node accepted-message material.
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("observation/gates persisted data")
	}
	for _, private := range s.Accounts[0].AddressKeys {
		if strings.Contains(string(a), private) {
			t.Fatal("private source in database")
		}
	}
	// Persisting the accepted sequence/digest must not persist the payload.
	encoded, _ := json.Marshal(s)
	if _, err := (&node.Repository{Database: f.app.Database}).Record(f.ctx, f.nodeID, &pb.AgentMessage{NodeId: f.nodeID, Sequence: 1, MessageId: "private-observation", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_ConnectionStatus{ConnectionStatus: &pb.ConnectionStatus{SnapshotJson: encoded}}}); err != nil {
		t.Fatal(err)
	}
	persisted, _ := json.Marshal(portalDatabaseSnapshot(t, f.app))
	for _, private := range s.Accounts[0].AddressKeys {
		if strings.Contains(string(persisted), private) {
			t.Fatal("accepted-message path persisted source")
		}
	}
	directory := t.TempDir()
	created, err := f.app.backupService.Source.Create(f.ctx, directory, "device_privacy_backup", "owner", "device-privacy-test-password")
	if err != nil {
		t.Fatal(err)
	}
	_, err = archive.Examine(f.ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), "device-privacy-test-password", func(_ archive.Result, work string) error {
		data, err := os.ReadFile(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		for _, private := range s.Accounts[0].AddressKeys {
			if strings.Contains(string(data), private) {
				t.Error("source appeared in backup database")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := f.owner.call("PUT", "/api/v1/users/"+u.ID+"/login", map[string]any{"enabled": true, "password": portalPassword}); r.Code != 204 {
		t.Fatal(r.Body.String())
	}
	client := portalLogin(t, f.app, u.Username, portalPassword)
	response := client.call("GET", "/api/v1/portal/devices-online?userId=foreign", nil)
	if response.Code != 200 || strings.Contains(response.Body.String(), "198.51.100.17") {
		t.Fatal("portal leaked source")
	}
	var me portal.Profile
	response = client.call("GET", "/api/v1/portal/me", nil)
	if json.Unmarshal(response.Body.Bytes(), &me) != nil || me.DeviceLimit == nil || *me.DeviceLimit != 1 || me.OnlineDevices == nil || *me.OnlineDevices != 3 {
		t.Fatal("portal profile lacks limit")
	}
	input.DeviceLimit = 4
	if r := f.owner.call("PUT", "/api/v1/users/"+u.ID, input); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	command, _ = o.Command(f.ctx, f.nodeID)
	gate, _ = devicegate.Decode(command.RequestJson)
	if len(gate.Entries) != 0 {
		t.Fatal("limit change not recomputed")
	}
	response = f.owner.call("POST", "/api/v1/users/"+u.ID+"/devices-online/reset", nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	_, _, count := o.Admission.State(u.ID, 1)
	if count == nil || *count != 3 {
		t.Fatal("reset did not immediately rebuild live admission")
	}
	input.DeviceLimit = 101
	if r := f.owner.call("PUT", "/api/v1/users/"+u.ID, input); r.Code != 400 {
		t.Fatal("invalid limit accepted")
	}
}
