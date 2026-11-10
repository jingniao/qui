package node

import (
	"context"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestWelcomeCarriesCataloguePort(t *testing.T) {
	p := newRecoveryPanel(t)
	node, leaf := enrolledAgentNode(t, p)

	// Case 1: CataloguePort configured and returns 443
	control1 := &Control{
		Repository: p.Repository,
		Authority:  p.authority,
		CataloguePort: func(ctx context.Context) uint32 {
			return 443
		},
	}
	stream1 := newFakeAgentStream(t, leaf, node.ID, "hello-1")
	go func() { _ = control1.Connect(stream1) }()

	select {
	case <-stream1.welcome:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for welcome message")
	}

	stream1.mu.Lock()
	var welcome1 *pb.Welcome
	for _, msg := range stream1.sent {
		if w := msg.GetWelcome(); w != nil {
			welcome1 = w
			break
		}
	}
	stream1.mu.Unlock()

	if welcome1 == nil {
		t.Fatal("welcome message not sent")
	}
	if welcome1.AgentCataloguePort != 443 {
		t.Fatalf("expected AgentCataloguePort=443, got %d", welcome1.AgentCataloguePort)
	}

	// Case 2: CataloguePort not configured (nil) -> 0
	control2 := &Control{
		Repository: p.Repository,
		Authority:  p.authority,
	}
	stream2 := newFakeAgentStream(t, leaf, node.ID, "hello-2")
	go func() { _ = control2.Connect(stream2) }()

	select {
	case <-stream2.welcome:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for welcome message")
	}

	stream2.mu.Lock()
	var welcome2 *pb.Welcome
	for _, msg := range stream2.sent {
		if w := msg.GetWelcome(); w != nil {
			welcome2 = w
			break
		}
	}
	stream2.mu.Unlock()

	if welcome2 == nil {
		t.Fatal("welcome message not sent")
	}
	if welcome2.AgentCataloguePort != 0 {
		t.Fatalf("expected AgentCataloguePort=0, got %d", welcome2.AgentCataloguePort)
	}
}
