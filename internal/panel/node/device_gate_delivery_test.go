package node

import (
	"errors"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestDeviceGateDeliveryBreaksFeedbackAndPreservesRefresh(t *testing.T) {
	var delivery deviceGateDelivery
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	command := &pb.DeviceGateCommand{RequestJson: []byte(`{"ttlSeconds":180,"entries":[]}`)}
	calls := 0
	send := func(any) error { calls++; return nil }
	for i := range 1000 {
		if err := delivery.send(command, now.Add(time.Duration(i)*time.Millisecond), send); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("1000 identical status reports sent %d gates; want 1", calls)
	}
	if err := delivery.send(command, now.Add(time.Minute), send); err != nil || calls != 2 {
		t.Fatalf("unchanged gate did not renew: calls=%d err=%v", calls, err)
	}
	// A changed restriction, then its removal, must not wait for the next renewal.
	command.RequestJson = []byte(`{"ttlSeconds":180,"entries":[{"credentialId":"c","keys":["k"]}]}`)
	if err := delivery.send(command, now.Add(time.Minute+time.Second), send); err != nil || calls != 3 {
		t.Fatalf("changed gate was delayed: calls=%d err=%v", calls, err)
	}
	command.RequestJson = []byte(`{"ttlSeconds":180,"entries":[]}`)
	if err := delivery.send(command, now.Add(time.Minute+2*time.Second), send); err != nil || calls != 4 {
		t.Fatalf("gate removal was delayed: calls=%d err=%v", calls, err)
	}
}

func TestDeviceGateDeliveryRetriesAnUnsentChange(t *testing.T) {
	var delivery deviceGateDelivery
	now := time.Now()
	command := &pb.DeviceGateCommand{RequestJson: []byte(`{"entries":[]}`)}
	failure := errors.New("stream closed")
	if err := delivery.send(command, now, func(any) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	calls := 0
	if err := delivery.send(command, now, func(any) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("failed send suppressed retry: calls=%d err=%v", calls, err)
	}
}
