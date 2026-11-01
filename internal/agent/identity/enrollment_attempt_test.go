package identity

import (
	"context"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/pki"
	"testing"
)

// install.sh --node creates host-network/ and tuning/ before the first
// enrollment, so a fresh data directory with only those folders must still
// accept an enrollment.
func TestEnrollmentDirectoryAcceptsTheInstalledHelperDirectories(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"identity", "host-network", "tuning"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := requireEnrollmentDirectory(context.Background(), directory, false); err != nil {
		t.Fatalf("fresh installed directory rejected: %v", err)
	}
}

// The helpers leave their own files behind; enrollment checks the directories,
// not what an install, an uninstall or a helper wrote into them.
func TestEnrollmentDirectoryAcceptsHelperDirectoryContents(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"host-network", "tuning"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"host-network/desired.json", "host-network/status.json", "tuning/desired.json", "tuning/status.json"} {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), []byte(`{"request_id":"kept"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := requireEnrollmentDirectory(context.Background(), directory, false); err != nil {
		t.Fatalf("helper directory contents rejected: %v", err)
	}
}

func TestEnrollmentDirectoryRejectsUnknownMaterialAndAHelperDirectoryThatIsNotOne(t *testing.T) {
	for name, prepare := range map[string]func(*testing.T, string){
		"unknown file": func(t *testing.T, directory string) {
			if err := os.WriteFile(filepath.Join(directory, "agent.db"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"host-network file": func(t *testing.T, directory string) {
			if err := os.WriteFile(filepath.Join(directory, "host-network"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"tuning file": func(t *testing.T, directory string) {
			if err := os.WriteFile(filepath.Join(directory, "tuning"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"tuning symlink": func(t *testing.T, directory string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(directory, "tuning")); err != nil {
				t.Skip("directory symlinks unavailable", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			prepare(t, directory)
			if err := os.Mkdir(filepath.Join(directory, "identity"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := requireEnrollmentDirectory(context.Background(), directory, false); err == nil {
				t.Fatal("directory with leftover material accepted")
			}
		})
	}
}

// Resuming a retained result runs the same directory check, so a node that
// interrupted its first enrollment resumes with the helper files still there.
func TestResumedEnrollmentResultAcceptsTheInstalledHelperDirectories(t *testing.T) {
	const tokenDigest = "46f1e24f2d6dd8f9f84b0d8f0f3bc09b0b0c01a6ba25efcbcbb2cf3d0a0ae4a5"
	const controlDigest = "b4b6d1ad6a5b7b4c6c8b3d0d24b1cb2d1e2a8f1f9a0f3e4d5c6b7a8091a2b3c4"
	ctx := context.Background()
	root := t.TempDir()
	authority, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"host-network", "tuning"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"host-network/desired.json", "tuning/desired.json", "tuning/status.json"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(`{"request_id":"kept"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareEnrollmentAttempt(ctx, root, tokenDigest, controlDigest); err != nil {
		t.Fatalf("the installed data directory refused the enrollment attempt: %v", err)
	}
	encodedCSR, err := CSR(filepath.Join(root, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := pki.ParseCSR(encodedCSR)
	if err != nil {
		t.Fatal(err)
	}
	certificate, leaf, err := authority.IssueNode(request, "node-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := RetainEnrollmentResult(ctx, root, tokenDigest, controlDigest, "node-one", certificate, authority.PEM, leaf.NotAfter); err != nil {
		t.Fatal(err)
	}
	found, err := ResumeEnrollmentResult(ctx, root, tokenDigest, controlDigest)
	if err != nil || !found {
		t.Fatalf("resuming the retained result failed: found=%v err=%v", found, err)
	}
	for _, name := range []string{"client.crt", "node-ca.crt", "identity.json"} {
		if _, err := os.Stat(filepath.Join(root, "identity", name)); err != nil {
			t.Fatalf("resume did not publish identity/%s: %v", name, err)
		}
	}
}
