package identity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// install.sh --node creates host-network/ before the first enrollment, so a
// fresh data directory with only that folder must still accept an enrollment.
func TestEnrollmentDirectoryAcceptsTheInstalledHostNetworkDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, hostNetworkDirectoryName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requireEnrollmentDirectory(context.Background(), directory, false); err != nil {
		t.Fatalf("fresh installed directory rejected: %v", err)
	}
}

func TestEnrollmentDirectoryRejectsUnknownMaterialAndAHostNetworkFile(t *testing.T) {
	for name, prepare := range map[string]func(string) error{
		"unknown file": func(directory string) error {
			return os.WriteFile(filepath.Join(directory, "agent.db"), nil, 0o600)
		},
		"host-network file": func(directory string) error {
			return os.WriteFile(filepath.Join(directory, hostNetworkDirectoryName), nil, 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := prepare(directory); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(directory, "identity"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := requireEnrollmentDirectory(context.Background(), directory, false); err == nil {
				t.Fatal("directory with leftover material accepted")
			}
		})
	}
}
