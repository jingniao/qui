package node_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRuntimeSnapshotCalibrationAndViewerProjection(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := &node.Repository{Database: database}
	n, _, _, err := repo.CreateEnrollment(ctx, "private-server-name", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var published []node.RuntimeSnapshot
	store, err := node.NewRuntimeSnapshots(database, func(snapshot node.RuntimeSnapshot) { published = append(published, snapshot) })
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.List(ctx)
	if err != nil || len(initial.Items) != 1 || initial.Items[0].Status != "pending" {
		t.Fatalf("initial runtime = %+v, %v", initial, err)
	}
	message := &pb.AgentMessage{NodeId: n.ID, Sequence: 1, MessageId: "heartbeat", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{
		CpuPercent: proto.Float64(0), MemoryPercent: proto.Float64(32), DiskPercent: proto.Float64(15), HostUptimeSeconds: proto.Float64(200),
		BootId: proto.String("private-boot"), DefaultRouteInterface: proto.String("private-interface"),
		TuningCurrent: map[string]string{"private-setting": "private-value"}, CoreError: "private-error", CoreHealthy: proto.Bool(false),
		PublicIp: "192.0.2.31", ActiveConnectionsMeasured: proto.Bool(true), CurrentRevision: 7,
		Interfaces: []*pb.InterfaceCounters{{Name: "private-interface", ReceiveBytesPerSecond: proto.Float64(0), TransmitBytesPerSecond: proto.Float64(2048)}},
	}}}
	if _, err := repo.Record(ctx, n.ID, message); err != nil {
		t.Fatal(err)
	}
	if err := store.Refresh(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if len(published) != 1 || published[0].Node.Status != "degraded" || published[0].Node.Revision <= initial.Items[0].Revision || published[0].InventoryRevision <= initial.InventoryRevision {
		t.Fatalf("accepted heartbeat was not published with changed inventory address: %+v", published)
	}
	if err := repo.Update(ctx, n.ID, "private-server-name", false); err != nil {
		t.Fatal(err)
	}
	// A delayed heartbeat callback must re-read the committed node and keep it disabled.
	if err := store.Refresh(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Refresh(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if len(published) != 2 || published[1].Node.Enabled || published[1].Node.Status != "disabled" || published[1].Node.Revision <= published[0].Node.Revision {
		t.Fatalf("disable and duplicate callbacks: %+v", published)
	}
	if err := repo.Update(ctx, n.ID, "private-server-name", true); err != nil {
		t.Fatal(err)
	}
	// Skip the event callback to simulate the browser missing the last event.
	// The authenticated lightweight endpoint must still learn the offline state.
	authRepo := &auth.Repository{Database: database}
	if err := authRepo.CreateAdmin(ctx, auth.Admin{ID: "viewer", Username: "viewer", DisplayName: "Viewer", Role: "Viewer", Status: "active", PasswordHash: "fixture", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("viewer-token"))
	if err := authRepo.CreateSession(ctx, hex.EncodeToString(hash[:]), "viewer", "fixture", "local", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	authentication := &auth.Handler{Service: &auth.Service{Repository: authRepo, Settings: &system.Repository{Database: database}}}
	mux := http.NewServeMux()
	(&node.Handler{Auth: authentication}).Register(mux, &node.Handler{Runtime: store})
	response := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/nodes/runtime", nil)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request returned %d", response.Code)
	}
	response = httptest.NewRecorder()
	request.AddCookie(&http.Cookie{Name: "proxy_session", Value: "viewer-token"})
	mux.ServeHTTP(response, request)
	var got node.RuntimeSnapshotList
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || response.Code != http.StatusOK || len(got.Items) != 1 {
		t.Fatalf("runtime endpoint: %d %s: %v", response.Code, response.Body, err)
	}
	if got.Generation == "" || got.Generation != initial.Generation || got.Items[0].Status != "offline" || got.Items[0].Revision <= published[1].Node.Revision || !got.Items[0].Enabled {
		t.Fatalf("runtime calibration did not show committed offline state: %+v", got)
	}
	for _, data := range []string{response.Body.String(), string(mustRuntimeJSON(t, published[0]))} {
		if strings.Contains(data, "private-") || strings.Contains(data, "192.0.2.31") || strings.Contains(data, "tuningCurrent") || strings.Contains(data, "capabilities") {
			t.Fatalf("runtime projection exposed unrelated data: %s", data)
		}
		for _, want := range []string{`"cpu_percent":0`, `"memory_percent":32`, `"host_uptime_seconds":200`, `"transmit_bytes_per_second":2048`, `"receive_bytes_per_second":0`, `"active_connections":0`, `"current_revision":7`, `"core_healthy":false`} {
			if !strings.Contains(data, want) {
				t.Errorf("runtime projection omitted %s: %s", want, data)
			}
		}
	}
}

func mustRuntimeJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
