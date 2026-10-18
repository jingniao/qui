package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/lifecycle"
)

func TestActualAgentRefusesOwnedDataBeforeOpeningIdentityOrEnrollment(t *testing.T) {
	binary := os.Getenv("AGENT_BINARY")
	if binary == "" {
		t.Skip("set actual Agent executable")
	}
	root := t.TempDir()
	data := filepath.Join(root, "data")
	owner, err := lifecycle.Try(data)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	// Deliberately invalid identity must never be opened/replaced by a second
	// process; the ownership failure must precede all identity interpretation.
	sentinel := filepath.Join(data, "identity", "identity.json")
	original := []byte("retained invalid identity evidence")
	if err := os.WriteFile(sentinel, original, 0600); err != nil {
		t.Fatal(err)
	}
	encodedPath, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	configuration := filepath.Join(root, "agent.yaml")
	if err := os.WriteFile(configuration, []byte("data_directory: "+string(encodedPath)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(root, "token")
	if err := os.WriteFile(token, []byte("unused-registration-code"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range [][]string{{"--config", configuration}, {"--config", configuration, "--enroll", "--token-file", token}} {
		command := exec.CommandContext(ctx, binary, args...)
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "persistent store is locked") {
			t.Fatal("actual Agent bypassed ownership", err, string(output))
		}
		retained, err := os.ReadFile(sentinel)
		if err != nil || string(retained) != string(original) {
			t.Fatal("identity evidence changed", err)
		}
		for _, path := range []string{"configuration.key", "spool", "runtime", "identity/client.key"} {
			if _, err := os.Stat(filepath.Join(data, filepath.FromSlash(path))); !os.IsNotExist(err) {
				t.Fatal("second Agent created runtime state", path, err)
			}
		}
	}
}
