package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/release"
)

func TestPrepareAgentCLIRealSignedCandidatePreservesNodeAndRejectsUnsafeInputs(t *testing.T) {
	binary := os.Getenv("AGENT_BINARY")
	if binary == "" {
		t.Skip("set actual Agent executable built with version 1.0.0-storage-test")
	}
	artifact, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	nodeDir := filepath.Join(root, "node")
	identityDir := filepath.Join(nodeDir, "identity")
	authority, err := pki.LoadOrCreate(filepath.Join(root, "authority"))
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := identity.CSR(identityDir)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(csrPEM)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	certificate, _, err := authority.IssueNode(csr, "prepare-node")
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.SaveEnrollment(identityDir, "prepare-node", certificate, authority.PEM); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(nodeDir, "configuration.key"), bytes.Repeat([]byte{17}, 32))
	// Sentinels represent existing opaque state. Preparation must not open,
	// migrate or replace these stores, even if their contents are unreadable.
	for _, folder := range []string{"spool", "revisions", "runtime"} {
		if err := os.Mkdir(filepath.Join(nodeDir, folder), 0700); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(nodeDir, folder, "retained.enc"), []byte("original-"+folder))
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "agent.yaml")
	yaml := fmt.Sprintf("data_directory: node\nupdates:\n  catalogue: https://releases.invalid/control\n  keys:\n    - public_key: %s\n      components: [proxy-agent]\n", base64.StdEncoding.EncodeToString(public))
	write(configPath, []byte(yaml))
	manifestPath := filepath.Join(root, "manifest.json")
	digest := sha256.Sum256(artifact)
	now := time.Now().UTC()
	manifest := release.Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: "1.0.0-storage-test", OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 1, Bytes: int64(len(artifact)), SHA256: hex.EncodeToString(digest[:]), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	sign := func(m release.Manifest, key ed25519.PrivateKey) {
		t.Helper()
		signed, err := release.Sign(m, key)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(signed)
		if err != nil {
			t.Fatal(err)
		}
		write(manifestPath, data)
	}
	sign(manifest, private)
	args := []string{"prepare-agent", "--agent-config", configPath, "--manifest", manifestPath, "--artifact", binary}
	var output bytes.Buffer
	if err := run(args, &output); err == nil {
		t.Fatal("unadopted storage was prepared")
	}
	if _, err := os.Stat(filepath.Join(nodeDir, "releases")); !os.IsNotExist(err) {
		t.Fatal("unadopted preflight wrote release state", err)
	}
	if _, err := storage.Ensure(nodeDir, "prepare-node", agentstorage.Current("existing-agent")); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string][32]byte {
		t.Helper()
		files := map[string][32]byte{}
		if err := filepath.WalkDir(nodeDir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == filepath.Join(nodeDir, "releases") {
				return filepath.SkipDir
			}
			if !entry.IsDir() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				files[path] = sha256.Sum256(data)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return files
	}
	original := snapshot()
	for n := 0; n < 2; n++ {
		output.Reset()
		if err := run(args, &output); err != nil {
			t.Fatal(err)
		}
		if output.String() != "agent-prepared 1.0.0-storage-test "+manifest.SHA256+" storage-compatible\n" {
			t.Fatal("unexpected receipt", output.String())
		}
	}
	for _, mode := range []string{"untrusted", "revoked", "version", "digest", "component", "extra-flags"} {
		t.Run(mode, func(t *testing.T) {
			write(configPath, []byte(yaml))
			sign(manifest, private)
			testArgs := append([]string(nil), args...)
			m := manifest
			switch mode {
			case "untrusted":
				_, other, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				sign(m, other)
			case "revoked":
				write(configPath, []byte(yaml+"      revoked: true\n"))
			case "version":
				m.Version = "wrong-version"
				sign(m, private)
			case "digest":
				m.SHA256 = strings.Repeat("0", 64)
				sign(m, private)
			case "component":
				m.Component = "xray"
				sign(m, private)
			case "extra-flags":
				testArgs = append(testArgs, "--initialize-history=false")
			}
			output.Reset()
			if err := run(testArgs, &output); err == nil || output.Len() != 0 {
				t.Fatal("unsafe preparation reported success", err, output.String())
			}
		})
	}
	after := snapshot()
	if len(original) != len(after) {
		t.Fatal("preflight changed node storage files")
	}
	for path, hash := range original {
		if after[path] != hash {
			t.Fatal("preflight modified retained state", filepath.Base(path))
		}
	}
	entries, err := os.ReadDir(filepath.Join(nodeDir, "releases"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "candidates" && entry.Name() != "agent-prepared" {
			t.Fatal("preflight created installation/history state", entry.Name())
		}
	}
}
