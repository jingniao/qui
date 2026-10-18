package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestActualControlOnlyCapabilityIsInformationOnly(t *testing.T) {
	binary := os.Getenv("AGENT_BINARY")
	if binary == "" {
		t.Skip("set actual Agent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root := t.TempDir()
	command := exec.CommandContext(ctx, binary, "--control-only-contract")
	command.Dir = root
	data, err := command.Output()
	if err != nil || string(data) != "proxy-agent-control-only-v1 1.0.0-storage-test\n" {
		t.Fatal(string(data), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("capability query wrote state", entries, err)
	}
	for _, args := range [][]string{{"--control-only-contract", "--config="}, {"--control-only-contract", "--storage-contract"}, {"--control-only-contract", "--managed-worker"}, {"--control-only-contract", "extra"}} {
		if _, err := exec.CommandContext(ctx, binary, args...).Output(); err == nil {
			t.Fatal("mixed control capability query", args)
		}
	}
}
