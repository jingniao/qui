package main

import (
	"errors"
	"flag"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Enrollment is a terminal mode. It enrolls and exits, so it accepts exactly
// --config, --enroll and --token-file and refuses to be combined with the
// supervised, managed or certificate modes that keep a process running.
func TestEnrollmentAcceptsOnlyConfigEnrollAndTokenFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	token := filepath.Join(t.TempDir(), "enrollment.token")
	for _, item := range []struct {
		name string
		args []string
		want string
	}{
		{"no config or token file", []string{"--enroll"}, "enrollment requires only"},
		{"no token file", []string{"--config", configPath, "--enroll"}, "enrollment requires only"},
		{"no config", []string{"--enroll", "--token-file", token}, "enrollment requires only"},
		{"supervise", []string{"--config", configPath, "--enroll", "--token-file", token, "--supervise"}, "enrollment requires only"},
		{"initialize history", []string{"--config", configPath, "--enroll", "--token-file", token, "--initialize-agent-history"}, "enrollment requires only"},
		{"managed worker", []string{"--config", configPath, "--enroll", "--token-file", token, "--managed-worker"}, "enrollment requires only"},
		{"renew certificate", []string{"--config", configPath, "--enroll", "--token-file", token, "--renew-node-certificate"}, "certificate operation requires"},
		{"rotate key", []string{"--config", configPath, "--enroll", "--token-file", token, "--rotate-node-key"}, "certificate operation requires"},
		{"check config", []string{"--config", configPath, "--enroll", "--token-file", token, "--check-config"}, "enrollment requires only"},
		{"storage contract", []string{"--config", configPath, "--enroll", "--token-file", token, "--storage-contract"}, "enrollment requires only"},
		{"positional argument", []string{"--config", configPath, "--enroll", "--token-file", token, "unexpected"}, "enrollment requires only"},
		{"unreadable token file", []string{"--config", configPath, "--enroll", "--token-file", filepath.Join(t.TempDir(), "missing.token")}, ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			set := flag.NewFlagSet("proxy-agent", flag.ContinueOnError)
			set.SetOutput(io.Discard)
			f := registerFlags(set)
			if err := set.Parse(item.args); err != nil {
				t.Fatalf("unexpected parse failure: %v", err)
			}
			err := execute(set, f)
			if err == nil {
				t.Fatal("invalid enrollment invocation was accepted")
			}
			if item.want == "" {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("expected the token file to be read, got %v", err)
				}
				return
			}
			if !strings.Contains(err.Error(), item.want) {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}

// The old --enroll-only spelling is gone; --enroll itself is the enroll-and-exit
// mode now, so a removal of the terminal behaviour must fail this test.
func TestEnrollmentModeIsNotTheSupervisedOrManagedWorker(t *testing.T) {
	set := flag.NewFlagSet("proxy-agent", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	f := registerFlags(set)
	if err := set.Parse([]string{"--config", "missing-config.yaml", "--enroll", "--token-file", "missing.token"}); err != nil {
		t.Fatal(err)
	}
	if !*f.enroll {
		t.Fatal("--enroll did not select the enrollment mode")
	}
	if err := execute(set, f); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("enrollment must stop at the token file, got %v", err)
	}
}
