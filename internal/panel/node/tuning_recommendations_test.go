package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/panel/node/tuningadvice"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type mockAdviceSources struct {
	hasQUIC  bool
	relay    bool
	median   *float64
	quicErr  error
	relayErr error
	rttErr   error
}

func (m *mockAdviceSources) HasQUICInbound(context.Context, string) (bool, error) {
	return m.hasQUIC, m.quicErr
}
func (m *mockAdviceSources) RelayRole(context.Context, string) (bool, error) {
	return m.relay, m.relayErr
}
func (m *mockAdviceSources) MedianRTT(context.Context, string) (*float64, error) {
	return m.median, m.rttErr
}

func testCapabilitiesWithHost(memoryBytes uint64, cpuCores int32, virt string) *pb.Capabilities {
	return &pb.Capabilities{
		TuningSupported: true,
		Host: &pb.HostInfo{
			MemoryTotalBytes:    memoryBytes,
			CpuCores:            cpuCores,
			Virtualization:      virt,
			CongestionAvailable: []string{"cubic", "reno"},
			CongestionLoadable:  []string{"bbr"},
			QdiscAvailable:      []string{"pfifo_fast"},
			QdiscLoadable:       []string{"fq"},
			TuningKeys: []*pb.TuningKeyState{
				{Key: "net.ipv4.tcp_congestion_control", Present: true, Writable: true},
				{Key: "net.core.default_qdisc", Present: true, Writable: true},
				{Key: "net.ipv4.tcp_slow_start_after_idle", Present: true, Writable: true},
				{Key: "net.ipv4.tcp_notsent_lowat", Present: true, Writable: true},
				{Key: "net.ipv4.tcp_rmem", Present: true, Writable: true},
				{Key: "net.ipv4.tcp_wmem", Present: true, Writable: true},
				{Key: "net.core.rmem_max", Present: true, Writable: true},
				{Key: "net.core.wmem_max", Present: true, Writable: true},
				{Key: "net.ipv4.tcp_mtu_probing", Present: true, Writable: true},
			},
		},
	}
}

func TestRecommendationsEndpoint(t *testing.T) {
	f := newMachineHTTPFixture(t)
	ctx := context.Background()

	// Update node with capabilities and heartbeat
	caps := testCapabilitiesWithHost(500*1024*1024, 2, "kvm")
	hb := &pb.Heartbeat{
		TuningCurrent: map[string]string{
			"net.ipv4.tcp_congestion_control": "cubic",
			"net.core.default_qdisc":          "pfifo_fast",
			"net.ipv4.tcp_slow_start_after_idle": "1",
			"net.ipv4.tcp_notsent_lowat":      "200000",
			"net.core.rmem_max":               "212992",
			"net.core.wmem_max":               "212992",
			"net.ipv4.tcp_rmem":               "4096 87380 6291456",
			"net.ipv4.tcp_wmem":               "4096 16384 4194304",
			"net.ipv4.tcp_mtu_probing":        "0",
		},
	}
	capsData, _ := json.Marshal(caps)
	hbData, _ := json.Marshal(hb)
	if _, err := f.repository.Database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=?, heartbeat_json=? WHERE id=?", string(capsData), string(hbData), f.nodeID); err != nil {
		t.Fatal(err)
	}

	f.repository.AdviceSources = &mockAdviceSources{
		hasQUIC: false,
		relay:   false,
	}

	// 1. Unauthenticated -> 401
	r := f.do(t, "GET", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations", f.nodeID), "", "", "")
	if r.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", r.Code)
	}

	// 2. Viewer read -> 200 OK
	r = f.do(t, "GET", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations", f.nodeID), "", f.viewer, f.viewerCSRF)
	if r.Code != http.StatusOK {
		t.Fatalf("viewer read expected 200, got %d: %s", r.Code, r.Body.String())
	}
	var view RecommendationView
	if err := json.Unmarshal(r.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.RulesVersion != tuningadvice.RulesVersion {
		t.Fatalf("rulesVersion expected %s, got %s", tuningadvice.RulesVersion, view.RulesVersion)
	}
	if len(view.Items) == 0 {
		t.Fatal("expected recommendations, got 0")
	}

	// Check items not dismissed initially
	var foundBBR bool
	for _, it := range view.Items {
		if it.Key == "net.ipv4.tcp_congestion_control" {
			foundBBR = true
			if it.Value != "bbr" || it.Dismissed {
				t.Fatalf("expected bbr not dismissed, got %+v", it)
			}
		}
	}
	if !foundBBR {
		t.Fatal("expected BBR advice")
	}

	// 3. Non-existent node -> 404
	r = f.do(t, "GET", "/api/v1/nodes/non-existent/tuning/recommendations", "", f.viewer, f.viewerCSRF)
	if r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// 4. Viewer dismiss -> 403 Forbidden
	dismissBody := `{"key":"net.ipv4.tcp_congestion_control","value":"bbr"}`
	r = f.do(t, "POST", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations/dismiss", f.nodeID), dismissBody, f.viewer, f.viewerCSRF)
	if r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer dismiss, got %d", r.Code)
	}

	// 5. Owner dismiss without CSRF -> 403 CSRF failed
	r = f.do(t, "POST", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations/dismiss", f.nodeID), dismissBody, f.owner, "")
	if r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF, got %d", r.Code)
	}

	// 6. Owner dismiss invalid key -> 400
	r = f.do(t, "POST", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations/dismiss", f.nodeID), `{"key":"unknown.key","value":"bbr"}`, f.owner, f.ownerCSRF)
	if r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid key, got %d", r.Code)
	}

	// 7. Owner dismiss valid -> 200 OK
	r = f.do(t, "POST", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations/dismiss", f.nodeID), dismissBody, f.owner, f.ownerCSRF)
	if r.Code != http.StatusOK {
		t.Fatalf("expected 200 for owner dismiss, got %d: %s", r.Code, r.Body.String())
	}

	// Audit check: action "不采纳调优建议"
	entries := f.audit.recorded()
	var auditFound bool
	for _, e := range entries {
		if strings.Contains(e, "不采纳调优建议") {
			auditFound = true
			break
		}
	}
	if !auditFound {
		t.Fatalf("audit entry not found for dismiss, recorded: %v", entries)
	}

	// Read recommendations again: BBR must be marked Dismissed: true
	r = f.do(t, "GET", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations", f.nodeID), "", f.viewer, f.viewerCSRF)
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}
	if err := json.Unmarshal(r.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	for _, it := range view.Items {
		if it.Key == "net.ipv4.tcp_congestion_control" {
			if !it.Dismissed {
				t.Fatalf("expected BBR dismissed to be true, got %+v", it)
			}
		}
	}

	// 8. Auto-apply setting GET/PUT endpoints
	// Viewer GET -> 403
	r = f.do(t, "GET", "/api/v1/system/tuning-auto-apply", "", f.viewer, f.viewerCSRF)
	if r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer GET auto-apply, got %d", r.Code)
	}

	// Owner GET -> 200, enabled: false, revision: 0
	r = f.do(t, "GET", "/api/v1/system/tuning-auto-apply", "", f.owner, f.ownerCSRF)
	if r.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", r.Code)
	}
	var autoSetting struct {
		Enabled  bool  `json:"enabled"`
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &autoSetting); err != nil {
		t.Fatal(err)
	}
	if autoSetting.Enabled || autoSetting.Revision != 0 {
		t.Fatalf("expected enabled:false, revision:0, got %+v", autoSetting)
	}

	// Viewer PUT -> 403
	r = f.do(t, "PUT", "/api/v1/system/tuning-auto-apply", `{"enabled":true,"expectedRevision":0}`, f.viewer, f.viewerCSRF)
	if r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer PUT auto-apply, got %d", r.Code)
	}

	// Owner PUT conflict -> 409
	r = f.do(t, "PUT", "/api/v1/system/tuning-auto-apply", `{"enabled":true,"expectedRevision":99}`, f.owner, f.ownerCSRF)
	if r.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d", r.Code)
	}

	// Owner PUT success -> 200
	r = f.do(t, "PUT", "/api/v1/system/tuning-auto-apply", `{"enabled":true,"expectedRevision":0}`, f.owner, f.ownerCSRF)
	if r.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.Code, r.Body.String())
	}
	if err := json.Unmarshal(r.Body.Bytes(), &autoSetting); err != nil {
		t.Fatal(err)
	}
	if !autoSetting.Enabled || autoSetting.Revision != 1 {
		t.Fatalf("expected enabled:true, revision:1, got %+v", autoSetting)
	}

	// Audit check: "修改调优自动应用"
	entries = f.audit.recorded()
	auditFound = false
	for _, e := range entries {
		if strings.Contains(e, "修改调优自动应用") {
			auditFound = true
			break
		}
	}
	if !auditFound {
		t.Fatalf("audit entry not found for auto-apply change, recorded: %v", entries)
	}
}

func TestDismissalReappearsWhenValueChanges(t *testing.T) {
	f := newMachineHTTPFixture(t)
	ctx := context.Background()

	// Initial node: 500 MB memory -> buffer cap is 8388608
	caps := testCapabilitiesWithHost(500*1024*1024, 2, "kvm")
	hb := &pb.Heartbeat{
		TuningCurrent: map[string]string{
			"net.core.rmem_max": "212992",
		},
	}
	capsData, _ := json.Marshal(caps)
	hbData, _ := json.Marshal(hb)
	if _, err := f.repository.Database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=?, heartbeat_json=? WHERE id=?", string(capsData), string(hbData), f.nodeID); err != nil {
		t.Fatal(err)
	}
	f.repository.AdviceSources = &mockAdviceSources{}

	// Dismiss rmem_max with value 8388608
	r := f.do(t, "POST", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations/dismiss", f.nodeID), `{"key":"net.core.rmem_max","value":"8388608"}`, f.owner, f.ownerCSRF)
	if r.Code != http.StatusOK {
		t.Fatalf("dismiss failed: %d %s", r.Code, r.Body.String())
	}

	// Verify dismissed: true
	r = f.do(t, "GET", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations", f.nodeID), "", f.viewer, f.viewerCSRF)
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}
	var view RecommendationView
	_ = json.Unmarshal(r.Body.Bytes(), &view)
	var found bool
	for _, it := range view.Items {
		if it.Key == "net.core.rmem_max" {
			found = true
			if !it.Dismissed || it.Value != "8388608" {
				t.Fatalf("expected dismissed 8388608, got %+v", it)
			}
		}
	}
	if !found {
		t.Fatal("rmem_max not found")
	}

	// Now memory upgraded to 2GB -> recommended value changes to 33554432!
	caps2 := testCapabilitiesWithHost(2*1024*1024*1024, 2, "kvm")
	capsData2, _ := json.Marshal(caps2)
	if _, err := f.repository.Database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=? WHERE id=?", string(capsData2), f.nodeID); err != nil {
		t.Fatal(err)
	}

	// Verify recommendation reappears (dismissed: false with new value 33554432)
	r = f.do(t, "GET", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations", f.nodeID), "", f.viewer, f.viewerCSRF)
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}
	_ = json.Unmarshal(r.Body.Bytes(), &view)
	found = false
	for _, it := range view.Items {
		if it.Key == "net.core.rmem_max" {
			found = true
			if it.Dismissed {
				t.Fatalf("expected dismissed to be false after value change, got %+v", it)
			}
			if it.Value != "33554432" {
				t.Fatalf("expected value 33554432, got %s", it.Value)
			}
		}
	}
	if !found {
		t.Fatal("rmem_max not found in view")
	}
}

func TestAdviceCountInNodeList(t *testing.T) {
	f := newMachineHTTPFixture(t)
	ctx := context.Background()

	// Only 2 suggested keys: bbr and fq; 1 optional key: mtu_probing
	caps := &pb.Capabilities{
		TuningSupported: true,
		Host: &pb.HostInfo{
			MemoryTotalBytes:    8 * 1024 * 1024 * 1024,
			CpuCores:            4,
			Virtualization:      "kvm",
			CongestionAvailable: []string{"cubic"},
			CongestionLoadable:  []string{"bbr"},
			QdiscAvailable:      []string{"pfifo_fast"},
			QdiscLoadable:       []string{"fq"},
			TuningKeys: []*pb.TuningKeyState{
				{Key: "net.ipv4.tcp_congestion_control", Present: true, Writable: true},
				{Key: "net.core.default_qdisc", Present: true, Writable: true},
				{Key: "net.ipv4.tcp_mtu_probing", Present: true, Writable: true},
			},
		},
	}
	hb := &pb.Heartbeat{
		TuningCurrent: map[string]string{
			"net.ipv4.tcp_congestion_control": "cubic",
			"net.core.default_qdisc":          "pfifo_fast",
			"net.ipv4.tcp_mtu_probing":        "0",
		},
	}
	capsData, _ := json.Marshal(caps)
	hbData, _ := json.Marshal(hb)
	if _, err := f.repository.Database.ExecContext(ctx, "UPDATE nodes SET capabilities_json=?, heartbeat_json=? WHERE id=?", string(capsData), string(hbData), f.nodeID); err != nil {
		t.Fatal(err)
	}
	f.repository.AdviceSources = &mockAdviceSources{relay: true}

	// Node List should show tuningAdviceCount == 2 (bbr + fq suggested; mtu_probing is optional so not counted)
	nodes, _, err := f.repository.List(ctx, "", 1, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].TuningAdviceCount != 2 {
		t.Fatalf("expected tuningAdviceCount == 2 in List, got %d", nodes[0].TuningAdviceCount)
	}

	// Repository.Get should also show tuningAdviceCount == 2
	n, err := f.repository.Get(ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if n.TuningAdviceCount != 2 {
		t.Fatalf("expected tuningAdviceCount == 2 in Get, got %d", n.TuningAdviceCount)
	}

	// Dismiss 1 suggested advice (bbr)
	r := f.do(t, "POST", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations/dismiss", f.nodeID), `{"key":"net.ipv4.tcp_congestion_control","value":"bbr"}`, f.owner, f.ownerCSRF)
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}

	// tuningAdviceCount should decrease to 1
	n, err = f.repository.Get(ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if n.TuningAdviceCount != 1 {
		t.Fatalf("expected tuningAdviceCount == 1 after dismissing 1 advice, got %d", n.TuningAdviceCount)
	}

	// Dismiss the other suggested advice (fq)
	r = f.do(t, "POST", fmt.Sprintf("/api/v1/nodes/%s/tuning/recommendations/dismiss", f.nodeID), `{"key":"net.core.default_qdisc","value":"fq"}`, f.owner, f.ownerCSRF)
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}

	n, err = f.repository.Get(ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if n.TuningAdviceCount != 0 {
		t.Fatalf("expected tuningAdviceCount == 0 after dismissing both, got %d", n.TuningAdviceCount)
	}
}

func TestAutoApplyOnlyForFreshNodes(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	audit := &machineAudit{}
	repo := &Repository{Database: database}
	control := &Control{Repository: repo, Audit: audit}

	capsMsg := &pb.AgentMessage{
		Sequence:  1,
		Timestamp: timestamppb.Now(),
		Payload: &pb.AgentMessage_Capabilities{
			Capabilities: testCapabilitiesWithHost(500*1024*1024, 2, "kvm"),
		},
	}

	// 1. Setting disabled: fresh node connects and reports capabilities
	n1, _, _, err := repo.CreateEnrollment(ctx, "Node 1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Consume enrollment so node is online/enrolled
	if _, err := database.ExecContext(ctx, "UPDATE nodes SET status='online', enabled=1 WHERE id=?", n1.ID); err != nil {
		t.Fatal(err)
	}
	capsMsg.NodeId = n1.ID
	capsMsg.MessageId = "msg-1"
	if _, err := repo.Record(ctx, n1.ID, capsMsg); err != nil {
		t.Fatal(err)
	}
	if err := control.maybeAutoApplyTuning(ctx, n1.ID, capsMsg.GetCapabilities(), true); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_tuning WHERE node_id=?", n1.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expected 0 tuning requests when disabled, got %d", count)
	}

	// 2. Enable auto-apply setting in system_settings
	nowStamp := timestamp(time.Now())
	settingJSON := fmt.Sprintf(`{"enabled":true,"revision":1,"updatedAt":"%s","updatedBy":"owner"}`, nowStamp)
	if _, err := database.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('tuning_auto_apply', ?)", settingJSON); err != nil {
		t.Fatal(err)
	}

	// Existing node (n1 already reported capabilities before) sends capabilities again
	// wasFirstCapabilities = false
	capsMsg.Sequence = 2
	capsMsg.MessageId = "msg-2"
	if _, err := repo.Record(ctx, n1.ID, capsMsg); err != nil {
		t.Fatal(err)
	}
	if err := control.maybeAutoApplyTuning(ctx, n1.ID, capsMsg.GetCapabilities(), false); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_tuning WHERE node_id=?", n1.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expected 0 tuning requests for existing node, got %d", count)
	}

	// 3. Fresh node enrolls after setting enabled
	n2, _, _, err := repo.CreateEnrollment(ctx, "Node 2", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "UPDATE nodes SET status='online', enabled=1 WHERE id=?", n2.ID); err != nil {
		t.Fatal(err)
	}
	capsMsg.NodeId = n2.ID
	capsMsg.Sequence = 1
	capsMsg.MessageId = "msg-3"
	if _, err := repo.Record(ctx, n2.ID, capsMsg); err != nil {
		t.Fatal(err)
	}
	// Fresh capability report
	if err := control.maybeAutoApplyTuning(ctx, n2.ID, capsMsg.GetCapabilities(), true); err != nil {
		t.Fatal(err)
	}

	// Verify node_tuning row created with status='pending'
	var status, desiredJSON, reqID string
	err = database.QueryRowContext(ctx, "SELECT status, desired_json, request_id FROM node_tuning WHERE node_id=?", n2.ID).Scan(&status, &desiredJSON, &reqID)
	if err != nil {
		t.Fatalf("expected pending tuning record for fresh node: %v", err)
	}
	if status != "pending" || reqID == "" {
		t.Fatalf("expected status 'pending', got %s (%s)", status, reqID)
	}
	var desired map[string]string
	if err := json.Unmarshal([]byte(desiredJSON), &desired); err != nil || len(desired) == 0 {
		t.Fatalf("invalid desired json: %s", desiredJSON)
	}
	// Check bbr is in desired
	if desired["net.ipv4.tcp_congestion_control"] != "bbr" {
		t.Fatalf("expected bbr in desired, got %v", desired)
	}

	// Verify audit log has "自动应用调优推荐" with operator "auto"
	entries := audit.recorded()
	var autoAuditFound bool
	for _, e := range entries {
		if strings.Contains(e, "自动应用调优推荐") && strings.Contains(e, "auto") {
			autoAuditFound = true
			break
		}
	}
	if !autoAuditFound {
		t.Fatalf("expected auto audit entry, got %v", entries)
	}

	// If n2 sends capabilities again, it must not create another tuning request or override
	capsMsg.Sequence = 2
	capsMsg.MessageId = "msg-4"
	if _, err := repo.Record(ctx, n2.ID, capsMsg); err != nil {
		t.Fatal(err)
	}
	if err := control.maybeAutoApplyTuning(ctx, n2.ID, capsMsg.GetCapabilities(), false); err != nil {
		t.Fatal(err)
	}
	var currentReqID string
	if err := database.QueryRowContext(ctx, "SELECT request_id FROM node_tuning WHERE node_id=?", n2.ID).Scan(&currentReqID); err != nil || currentReqID != reqID {
		t.Fatalf("tuning request changed: %s vs %s", currentReqID, reqID)
	}
}