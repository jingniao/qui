package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type brokenHeader struct{}

func (brokenHeader) Write(p []byte) (int, error) {
	if bytes.HasPrefix(p, []byte("checker-exit:")) {
		return 0, errors.New("output closed")
	}
	return len(p), nil
}

func TestPreflightOutputAndFailures(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup")
	checker := filepath.Join(dir, "checker")
	if err := os.WriteFile(backup, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checker, []byte("#!/bin/sh\nprintf 'newer database\\n'\nexit 3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"report", "missing-checker", "missing-backup", "output-failure"} {
		t.Run(kind, func(t *testing.T) {
			check := filepath.Join(dir, kind)
			if err := os.Mkdir(check, 0700); err != nil {
				t.Fatal(err)
			}
			args := []string{"preflight", "--check-directory", check, "--backup", backup, "--checker", checker}
			if kind == "missing-checker" {
				args[len(args)-1] += ".missing"
			}
			if kind == "missing-backup" {
				args[4] += ".missing"
			}
			var out, diagnostic bytes.Buffer
			code := 0
			if kind == "output-failure" {
				code = run(context.Background(), args, brokenHeader{}, &diagnostic)
			} else {
				code = run(context.Background(), args, &out, &diagnostic)
			}
			want := 0
			if kind == "missing-backup" || kind == "output-failure" {
				want = 70
			}
			if code != want {
				t.Fatalf("code=%d want=%d output=%q err=%q", code, want, out.String(), diagnostic.String())
			}
			if kind == "report" && out.String() != "checker-exit: 3\nnewer database\n" {
				t.Fatal(out.String())
			}
			if kind == "missing-checker" && !bytes.HasPrefix(out.Bytes(), []byte("checker-exit: 127\n")) {
				t.Fatal(out.String())
			}
			if _, err := os.Stat(check); !os.IsNotExist(err) {
				t.Fatalf("check remains: %v", err)
			}
		})
	}
}
