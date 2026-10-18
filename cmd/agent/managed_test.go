package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActualManagedAgentRejectsInvalidChannelBeforeCreatingState(t *testing.T) {
	binary := os.Getenv("AGENT_BINARY")
	if binary == "" {
		t.Skip("set actual Agent")
	}
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	path, _ := json.Marshal(dataDir)
	configuration := filepath.Join(root, "agent.yaml")
	if err := os.WriteFile(configuration, []byte("data_directory: "+string(path)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, input := range []string{"", "{}\n", strings.Repeat("x", 4097), `{"kind":"agent-start-v1","nonce":"` + strings.Repeat("a", 64) + `","node_id":"node","revision":0,"version":"wrong-version"}` + "\n"} {
		command := exec.CommandContext(ctx, binary, "--managed-worker", "--config", configuration)
		command.Stdin = strings.NewReader(input)
		if output, err := command.CombinedOutput(); err == nil || bytes.Contains(output, []byte("agent-ready-v1")) {
			t.Fatal("invalid managed channel confirmed", err)
		}
	}
	for _, extra := range []string{"--enroll=false", "--check-config=false", "--version", "--token-file=", "--supervise", "--initialize-agent-history"} {
		command := exec.CommandContext(ctx, binary, "--managed-worker", "--config", configuration, extra)
		if output, err := command.CombinedOutput(); err == nil || bytes.Contains(output, []byte("agent-ready-v1")) {
			t.Fatal("mixed managed flags accepted", extra, err)
		}
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatal("invalid startup initialized persistent state", err)
	}
}
