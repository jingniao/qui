package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalCLI(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	d := t.TempDir()
	os.Chmod(d, 0700)
	var out, diag bytes.Buffer
	args := []string{"journal-write", "--snapshot", d, "--component", "agent", "--previous", "$(touch owned)", "--target", "中文 =", "--was-active", "true", "--phase", "prepared"}
	if code := run(args, strings.NewReader(""), &out, &diag); code != 0 {
		t.Fatal(code, diag.String())
	}
	if code := run([]string{"journal-read", "--file", filepath.Join(d, "journal"), "--backups", d}, strings.NewReader(""), &out, &diag); code != 0 {
		t.Fatal(code, diag.String())
	}
	fields := strings.Split(out.String(), "\x00")
	if len(fields) != 10 || fields[0] != "agent" || fields[1] != "$(touch owned)" || fields[2] != "中文 =" || fields[3] != "true" || fields[6] != "prepared" {
		t.Fatal(fields)
	}
	args[4] = "panel"
	args = append(args, "--core-request", "")
	if code := run(args, strings.NewReader(""), &out, &diag); code != 70 {
		t.Fatal(code)
	}
}
