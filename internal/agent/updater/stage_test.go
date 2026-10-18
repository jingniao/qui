package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/pkg/contracts/release"
	"runtime"
	"sync"
	"testing"
	"time"
)

func stageFixture(t *testing.T) ([]byte, release.Signed, release.Policy) {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(artifact)
	now := time.Now().UTC()
	manifest := release.Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: "stage-test", OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 1, Bytes: int64(len(artifact)), SHA256: hex.EncodeToString(digest[:]), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	signed, err := release.Sign(manifest, private)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, signed, release.Policy{Component: manifest.Component, OS: manifest.OS, Architecture: manifest.Architecture, Now: now, Keys: map[string]release.TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{manifest.Component}}}}
}
func TestStageRealExecutableConcurrentReplayAndCorruption(t *testing.T) {
	artifact, signed, policy := stageFixture(t)
	directory := filepath.Join(t.TempDir(), "releases")
	var wait sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := Stage(context.Background(), directory, signed, policy, bytes.NewReader(artifact))
			failures <- err
		}()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 1 || files[0].Name() != signed.Manifest.SHA256+".bin" {
		t.Fatal("candidate collision or leftover partial file", files, err)
	}
	path := filepath.Join(directory, files[0].Name())
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(context.Background(), directory, signed, policy, bytes.NewReader(artifact)); !errors.Is(err, ErrArtifact) {
		t.Fatal("existing candidate implicitly overwritten", err)
	}
}
func TestStageRejectsSizeDigestScriptsWrongPlatformAndCancellation(t *testing.T) {
	artifact, signed, policy := stageFixture(t)
	for name, data := range map[string][]byte{"truncated": artifact[:len(artifact)-1], "extra": append(append([]byte(nil), artifact...), 0), "changed": append([]byte("changed"), artifact[7:]...)} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if _, err := Stage(context.Background(), directory, signed, policy, bytes.NewReader(data)); err == nil {
				t.Fatal("bad bytes staged")
			}
			entries, _ := os.ReadDir(directory)
			if len(entries) != 0 {
				t.Fatal("partial bad artifact left")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Stage(ctx, t.TempDir(), signed, policy, bytes.NewReader(artifact)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	policy.Keys = map[string]release.TrustedKey{release.KeyID(public): {PublicKey: public, Components: []string{"proxy-agent"}}}
	for name, body := range map[string][]byte{"script": []byte("#!/bin/sh\necho arbitrary\n"), "garbage": bytes.Repeat([]byte("x"), 1024)} {
		t.Run(name, func(t *testing.T) {
			m := signed.Manifest
			sum := sha256.Sum256(body)
			m.Bytes = int64(len(body))
			m.SHA256 = hex.EncodeToString(sum[:])
			s, err := release.Sign(m, private)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Stage(context.Background(), t.TempDir(), s, policy, bytes.NewReader(body)); !errors.Is(err, ErrArtifact) {
				t.Fatal("non-executable release accepted", err)
			}
		})
	}
	m := signed.Manifest
	if m.OS == "windows" {
		m.OS = "linux"
	} else {
		m.OS = "windows"
	}
	s, _ := release.Sign(m, private)
	policy.OS = m.OS
	if _, err := Stage(context.Background(), t.TempDir(), s, policy, bytes.NewReader(artifact)); !errors.Is(err, ErrArtifact) {
		t.Fatal("signed platform label overrode executable format", err)
	}
}

func TestStageRejectsCandidateSymlinkEvenWhenTargetBytesAreValid(t *testing.T) {
	artifact, signed, policy := stageFixture(t)
	directory := t.TempDir()
	target := filepath.Join(directory, "other.bin")
	if err := os.WriteFile(target, artifact, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.bin", filepath.Join(directory, signed.Manifest.SHA256+".bin")); err != nil {
		t.Skip("symlink creation unavailable", err)
	}
	if _, err := Stage(context.Background(), directory, signed, policy, bytes.NewReader(artifact)); !errors.Is(err, ErrArtifact) {
		t.Fatal("candidate link accepted", err)
	}
}
