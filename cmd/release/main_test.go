package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOfflineReleaseKeySignVerifyAndStageRealExecutable(t *testing.T) {
	directory := t.TempDir()
	private := filepath.Join(directory, "release.key")
	public := filepath.Join(directory, "release.pub")
	manifest := filepath.Join(directory, "release.json")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := &bytes.Buffer{}
	if err := run([]string{"keygen", "--private-key", private, "--public-key", public}, output); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"keygen", "--private-key", private, "--public-key", public}, output); err == nil {
		t.Fatal("signing key replaced")
	}
	if err := run([]string{"sign", "--private-key", private, "--artifact", binary, "--out", manifest, "--component", "proxy-agent", "--version", "1.0.0", "--sequence", "12", "--os", runtime.GOOS, "--arch", runtime.GOARCH}, output); err != nil {
		t.Fatal(err)
	}
	common := []string{"--public-key", public, "--manifest", manifest, "--component", "proxy-agent", "--minimum-sequence", "12", "--os", runtime.GOOS, "--arch", runtime.GOARCH}
	if err := run(append([]string{"verify"}, common...), output); err != nil {
		t.Fatal(err)
	}
	stage := append(append([]string{"stage"}, common...), "--artifact", binary, "--out", filepath.Join(directory, "candidates"))
	if err := run(stage, output); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(private)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), bytes.TrimSpace(key)) {
		t.Fatal("private signing key printed")
	}
}

func TestKeyPairCreationFailureDoesNotLeaveOrReplaceOneKey(t *testing.T) {
	directory := t.TempDir()
	private := filepath.Join(directory, "release.key")
	public := filepath.Join(directory, "release.pub")
	if err := os.WriteFile(public, []byte("existing public"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"keygen", "--private-key", private, "--public-key", public}, &bytes.Buffer{}); err == nil {
		t.Fatal("existing public key replaced")
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatal("orphan private key", err)
	}
	data, err := os.ReadFile(public)
	if err != nil || string(data) != "existing public" {
		t.Fatal("public output changed", err)
	}
	if err := run([]string{"keygen", "--private-key", private, "--public-key", filepath.Join(directory, "missing", "release.pub")}, &bytes.Buffer{}); err == nil {
		t.Fatal("missing parent accepted")
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatal("orphan after public creation failure", err)
	}
	if err := run([]string{"keygen", "--private-key", private, "--public-key", filepath.Join(directory, ".", "release.key")}, &bytes.Buffer{}); err == nil {
		t.Fatal("equivalent output paths accepted")
	}
}
