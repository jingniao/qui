package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"proxycontrol/internal/agent/agentprocess"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/updater"
)

type managedParentFixture struct {
	Images   string
	Image    updater.Image
	Config   string
	Node     string
	Revision int64
}

func verifyManagedParentDeath(t *testing.T, fixture managedParentFixture, dataDirectory string) {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestManagedAgentParentChild$")
	command.Env = append(os.Environ(), "PROXY_MANAGED_PARENT_FIXTURE="+string(data))
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	reader := bufio.NewScanner(output)
	if !reader.Scan() || reader.Text() != "ready" {
		command.Process.Kill()
		command.Wait()
		t.Fatal("supervisor helper did not confirm actual Agent", reader.Text(), reader.Err())
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("supervisor helper did not terminate abruptly")
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		owner, err := lifecycle.Try(dataDirectory)
		if err == nil {
			owner.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("orphan Agent retained data after parent death", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
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
