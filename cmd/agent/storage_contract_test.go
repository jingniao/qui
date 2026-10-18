package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/pkg/contracts/agentstorage"
	"testing"
	"time"
)

func TestActualAgentStorageDescriptorDoesNotLoadConfigurationOrWriteData(t *testing.T) {
	binary := os.Getenv("AGENT_BINARY")
	if binary == "" {
		t.Skip("set actual Agent executable")
	}
	root := t.TempDir()
	identity := filepath.Join(root, "state", "agent", "identity")
	if err := os.MkdirAll(identity, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(identity, "client.key")
	original := []byte("existing private identity must stay untouched")
	if err := os.WriteFile(sentinel, original, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--storage-contract")
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	c, err := agentstorage.Decode(data)
	if err != nil || c.AgentVersion != "1.0.0-storage-test" {
		t.Fatal(c, err)
	}
	if bytes.Contains(data, []byte("agent_recovery_protocol")) || c.AgentRecoveryProtocol != 0 {
		t.Fatal("legacy query exposes an unknown extension")
	}
	extended, err := exec.CommandContext(ctx, binary, "--storage-contract", "--storage-contract-version=2").Output()
	if err != nil {
		t.Fatal(err)
	}
	newContract, err := agentstorage.Decode(extended)
	if err != nil || newContract.AgentRecoveryProtocol != 1 {
		t.Fatal("extended recovery capability missing", err)
	}
	newContract.AgentRecoveryProtocol = 0
	if newContract != c {
		t.Fatal("descriptor query changed storage contract")
	}
	after, err := os.ReadFile(sentinel)
	if err != nil || sha256.Sum256(after) != sha256.Sum256(original) {
		t.Fatal("descriptor rewrote private state", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "state", "agent"))
	if err != nil || len(entries) != 1 {
		t.Fatal("descriptor initialized Agent storage", entries, err)
	}
	for _, args := range [][]string{{"--storage-contract", "--config", "missing-config.yaml"}, {"--storage-contract", "--enroll"}, {"--storage-contract", "--token-file", "missing-token"}, {"--storage-contract", "--version"}, {"--storage-contract", "unexpected"}, {"--storage-contract", "--config="}, {"--storage-contract", "--token-file="}, {"--storage-contract", "--enroll=false"}, {"--storage-contract", "--check-config=false"}, {"--storage-contract", "--version=false"}} {
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = root
		if _, err := command.Output(); err == nil {
			t.Fatal("descriptor combined with runtime arguments", args)
		}
	}
	for _, args := range [][]string{{"--storage-contract-version=2"}, {"--storage-contract", "--storage-contract-version=0"}, {"--storage-contract", "--storage-contract-version=3"}, {"--storage-contract", "--storage-contract-version=2", "--config="}} {
		if _, err := exec.CommandContext(ctx, binary, args...).Output(); err == nil {
			t.Fatal("invalid descriptor version accepted", args)
		}
	}
}
