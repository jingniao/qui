package node

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

type countingAdvice struct{ reads atomic.Int32 }

func (a *countingAdvice) HasQUICInbound(context.Context, string) (bool, error) {
	a.reads.Add(1)
	return false, nil
}
func (a *countingAdvice) RelayRole(context.Context, string) (bool, error) {
	a.reads.Add(1)
	return false, nil
}
func (a *countingAdvice) MedianRTT(context.Context, string) (*float64, error) {
	a.reads.Add(1)
	return nil, nil
}

// Device-gate notifications run for every connected Agent. They must not sort
// 24 hours of latency samples just to check the Agent's supported capabilities.
func TestControlGateRefreshDoesNotComputeTuningAdvice(t *testing.T) {
	p := newRecoveryPanel(t)
	n, leaf := enrolledAgentNode(t, p)
	caps := testCapabilitiesWithHost(1<<30, 1, "kvm")
	caps.CoreName, caps.DeviceGateSupported = "xray", true
	encoded, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(encoded), n.ID); err != nil {
		t.Fatal(err)
	}
	advice := &countingAdvice{}
	p.Repository.AdviceSources = advice
	changes := make(chan struct{}, 1)
	read := make(chan struct{}, 1)
	control := &Control{Repository: p.Repository, Authority: p.authority,
		GateChanges: func() (<-chan struct{}, func()) { return changes, func() {} },
		DeviceGates: func(context.Context, string) (*pb.DeviceGateCommand, error) {
			read <- struct{}{}
			return &pb.DeviceGateCommand{RequestJson: []byte(`{"entries":[]}`)}, nil
		},
	}
	stream := newFakeAgentStream(t, leaf, n.ID, "gate-read")
	stream.hello.GetHello().DeviceGateSupported = true
	done := make(chan error, 1)
	go func() { done <- control.Connect(stream) }()
	defer func() {
		control.Disconnect(n.ID)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("control did not stop")
		}
	}()
	waitForWelcome(t, stream)
	changes <- struct{}{}
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("gate command was not read")
	}
	if got := advice.reads.Load(); got != 0 {
		t.Fatalf("control-only gate refresh triggered %d tuning advice reads", got)
	}
}
