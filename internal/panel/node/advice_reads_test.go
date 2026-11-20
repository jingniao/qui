package node

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func setAdviceCapabilities(t *testing.T, r *Repository, id string) *countingAdvice {
	t.Helper()
	caps := testCapabilitiesWithHost(1<<30, 2, "kvm")
	caps.CoreName = "xray"
	raw, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), id); err != nil {
		t.Fatal(err)
	}
	advice := &countingAdvice{}
	r.AdviceSources = advice
	return advice
}

func TestLatencyDoesNotComputeTuningAdvice(t *testing.T) {
	r, id := latencyFixture(t)
	advice := setAdviceCapabilities(t, r, id)
	result := readLatency(t, r, id, 24)
	if len(result.Series) != 2 {
		t.Fatalf("scheduled targets missing: %+v", result.Series)
	}
	if got := advice.reads.Load(); got != 0 {
		t.Fatalf("latency triggered %d tuning advice reads", got)
	}
}

func TestTuningStatusDoesNotComputeAdvice(t *testing.T) {
	f := newMachineHTTPFixture(t)
	advice := setAdviceCapabilities(t, f.repository, f.nodeID)
	response := f.do(t, "GET", "/api/v1/nodes/"+f.nodeID+"/tuning", "", f.viewer, f.viewerCSRF)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if got := advice.reads.Load(); got != 0 {
		t.Fatalf("tuning status triggered %d advice reads", got)
	}
}

func TestTuningRecommendationsComputeAdviceOnce(t *testing.T) {
	f := newMachineHTTPFixture(t)
	advice := setAdviceCapabilities(t, f.repository, f.nodeID)
	if _, err := f.repository.Database.Exec("INSERT INTO node_meta(node_id,bandwidth_mbps) VALUES(?,100)", f.nodeID); err != nil {
		t.Fatal(err)
	}
	response := f.do(t, "GET", "/api/v1/nodes/"+f.nodeID+"/tuning/recommendations", "", f.viewer, f.viewerCSRF)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var view RecommendationView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) == 0 {
		t.Fatal("recommendations missing")
	}
	if reads, rtt := advice.reads.Load(), advice.rttReads.Load(); reads != 3 || rtt != 1 {
		t.Fatalf("recommendation read sources %d times, of which %d were RTT queries", reads, rtt)
	}
}

func TestTuningAdviceSkipsRTTWithoutBandwidth(t *testing.T) {
	r, id := latencyFixture(t)
	advice := setAdviceCapabilities(t, r, id)
	n, err := r.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if n.TuningAdviceCount == 0 {
		t.Fatal("node advice count missing")
	}
	if got := advice.rttReads.Load(); got != 0 {
		t.Fatalf("missing bandwidth still triggered %d RTT queries", got)
	}
}

func TestExpandedNodesPreserveSubscriptionAndPortalFieldsWithoutAdvice(t *testing.T) {
	r, id := latencyFixture(t)
	ctx := context.Background()
	advice := setAdviceCapabilities(t, r, id)
	if _, err := r.Database.Exec("UPDATE nodes SET status='online',connect_host='entry.example.com' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	group, err := r.SaveGroup(ctx, "", GroupInput{Name: "Access", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, ServerIDs: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := r.ExpandedNodes(ctx, []string{group.ID, group.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one deduplicated node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.ID != id || n.Name != "latency" || n.Status != "online" || n.ConnectAddress() != "entry.example.com" || n.Capabilities.GetCoreName() != "xray" {
		t.Fatalf("subscription or portal node fields changed: %+v", n)
	}
	if err := r.SubscriptionFlags(ctx, id, true, true); err != nil {
		t.Fatal(err)
	}
	nodes, err = r.ExpandedNodes(ctx, []string{group.ID})
	if err != nil || len(nodes) != 0 {
		t.Fatalf("maintenance node was not filtered: %v, error %v", nodes, err)
	}
	if got := advice.reads.Load(); got != 0 {
		t.Fatalf("node expansion triggered %d tuning advice reads", got)
	}
}
