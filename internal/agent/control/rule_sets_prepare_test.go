package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"proxycontrol/internal/agent/rulesets"
	"sync/atomic"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

type slowRuleSetRuntime struct {
	*fakeCoreRuntime
	queue    *spool.Spool
	prepares atomic.Int32
}

type cancelledRuleSetRuntime struct {
	*fakeCoreRuntime
	fetcher            *rulesets.Fetcher
	directory, address string
	prepared           chan struct{}
}

func (a *cancelledRuleSetRuntime) PrepareRuleSets(ctx context.Context, _ desiredstate.State) (*rulesets.Update, error) {
	update, err := a.fetcher.Ensure(ctx, a.directory, []rulesets.Need{{URL: a.address, SingBox: true}})
	if err != nil {
		return update, err
	}
	close(a.prepared)
	<-ctx.Done()
	return update, ctx.Err()
}
func (a *cancelledRuleSetRuntime) CleanupRuleSets(...desiredstate.State) {}

func TestRuleSetPreparationSessionCancellationDiscardsDownloads(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("SRS-new")) }))
	defer server.Close()
	base := &fakeCoreRuntime{version: "26.3.27"}
	_, cfg, manager, _ := updateHarness(t, base)
	a := &cancelledRuleSetRuntime{fakeCoreRuntime: base, directory: t.TempDir(), address: server.URL + "/cn.srs", prepared: make(chan struct{}), fetcher: &rulesets.Fetcher{Client: server.Client(), AllowHost: func(string) bool { return true }, AllowURL: func(string, bool) bool { return true }}}
	manager.Adapter = a
	next := desiredstate.State{SchemaVersion: 1, NodeID: "node-update", Revision: 2, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	data, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	_, stream := runSessionStream(t, cfg, manager, base, nil, time.Second, func(s *controlStream) { s.applyBeforeAck = &pb.ApplyCommand{DesiredStateJson: data} })
	select {
	case <-a.prepared:
	default:
		t.Fatal("download was never prepared")
	}
	entries, err := os.ReadDir(rulesets.Dir(a.directory))
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled session left download files", entries, err)
	}
	if manager.Current() != 1 {
		t.Fatal("cancelled session applied revision")
	}
	for _, message := range stream.sent {
		if message.GetApplyResult() != nil {
			t.Fatal("cancelled download reported an application result")
		}
	}
}

func (a *slowRuleSetRuntime) PrepareRuleSets(ctx context.Context, _ desiredstate.State) (*rulesets.Update, error) {
	a.prepares.Add(1)
	select {
	case <-time.After(3200 * time.Millisecond):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (a *slowRuleSetRuntime) CleanupRuleSets(...desiredstate.State) {}
func (a *slowRuleSetRuntime) Apply(ctx context.Context, c core.RenderedConfig) (core.ApplyResult, error) {
	result, err := a.fakeCoreRuntime.Apply(ctx, c)
	if err != nil {
		return result, err
	}
	err = a.queue.SaveTraffic(ctx, []traffic.Counter{{CoreEpoch: "new", Revision: c.Revision, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "http", UploadBytes: 10, DownloadBytes: 20, ObservedAt: time.Now().UTC()}})
	if err != nil {
		return result, err
	}
	_, err = a.queue.Enqueue(ctx, []access.Rollup{{Bucket: time.Now().UTC().Truncate(time.Minute), NodeID: "node-update", Revision: c.Revision, InboundProtocol: "http", Domain: "example.com", TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 1}})
	return result, err
}

func TestRuleSetPreparationKeepsHeartbeatsAndResultBeforeTelemetry(t *testing.T) {
	base := &fakeCoreRuntime{version: "26.3.27"}
	_, cfg, manager, _ := updateHarness(t, base)
	queue, err := spool.Open(context.Background(), t.TempDir(), "node-update", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	slow := &slowRuleSetRuntime{fakeCoreRuntime: base, queue: queue}
	manager.Adapter = slow
	next := desiredstate.State{SchemaVersion: 1, NodeID: "node-update", Revision: 2, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	data, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	_, stream := runSessionStream(t, cfg, manager, base, queue, 5*time.Second, func(s *controlStream) {
		s.applyBeforeAck = &pb.ApplyCommand{DesiredStateJson: data}
		go func() {
			for i := 0; i < 15; i++ {
				select {
				case <-s.ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
				select {
				case <-s.ctx.Done():
					return
				case s.incoming <- controlPacket{message: &pb.ServerMessage{NodeId: "node-update", Payload: &pb.ServerMessage_ApplyCommand{ApplyCommand: &pb.ApplyCommand{DesiredStateJson: data}}}}:
				}
			}
		}()
	})
	stream.mu.Lock()
	defer stream.mu.Unlock()
	heartbeats, resultAt, trafficAt, accessAt := 0, -1, -1, -1
	for i, message := range stream.sent {
		if message.GetHeartbeat() != nil && resultAt < 0 {
			heartbeats++
		}
		if result := message.GetApplyResult(); result != nil {
			if !result.Success || result.Revision != 2 {
				t.Fatalf("apply: %v", result)
			}
			resultAt = i
		}
		if message.GetTrafficBatch() != nil {
			trafficAt = i
		}
		if message.GetAccessBatch() != nil {
			accessAt = i
		}
	}
	if heartbeats < 3 {
		t.Errorf("download blocked heartbeats: got %d before apply result", heartbeats)
	}
	if heartbeats > 6 {
		t.Errorf("duplicate commands caused heartbeat flood: %d", heartbeats)
	}
	if slow.prepares.Load() != 1 {
		t.Errorf("duplicate downloads: %d", slow.prepares.Load())
	}
	if resultAt < 0 || trafficAt <= resultAt || accessAt <= resultAt {
		t.Fatalf("result must precede both telemetry types: result=%d traffic=%d access=%d", resultAt, trafficAt, accessAt)
	}
}
