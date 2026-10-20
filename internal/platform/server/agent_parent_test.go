package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"proxycontrol/internal/agent/agentprocess"
	"proxycontrol/internal/agent/updater"
)

type managedParentFixture struct {
	Images   string
	Image    updater.Image
	Config   string
	Node     string
	Revision int64
}

func TestManagedAgentParentChild(t *testing.T) {
	data := os.Getenv("PROXY_MANAGED_PARENT_FIXTURE")
	if data == "" {
		t.Skip("only run in parent-death fixture")
	}
	var fixture managedParentFixture
	if err := json.Unmarshal([]byte(data), &fixture); err != nil {
		t.Fatal(err)
	}
	process, err := agentprocess.Start(context.Background(), fixture.Images, fixture.Image, fixture.Config, fixture.Node, fixture.Revision, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Stop(context.Background())
	fmt.Println("ready")
	var stop [1]byte
	_, _ = os.Stdin.Read(stop[:])
}
