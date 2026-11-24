package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestEventCLI(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		input string
		code  int
	}{
		{[]string{"event", "--stage", "terminal", "--state", "failed", "--exit-code", "143"}, "line\n中文", 0},
		{[]string{"event", "--stage", "agent_install", "--state", "failed", "--exit-code", "null", "--error-json"}, `"<>&"`, 0},
		{[]string{"event", "--stage", "terminal", "--state", "success", "--exit-code", "null", "--error-json"}, `null`, 1},
		{[]string{"event", "--stage", "bad\n", "--state", "failed", "--exit-code", "0"}, "", 70},
		{[]string{"event", "--stage", "terminal", "--state", "failed", "--exit-code", "256"}, "", 70},
		{[]string{"journal-write", "--snapshot", "/unused", "--component", "bad", "--was-active", "true", "--phase", "prepared"}, "", 70},
		{[]string{"journal-write", "--snapshot", "/unused", "--component", "panel", "--was-active", "true", "--phase", "unknown"}, "", 70},
		{[]string{"discard", "--registry", "/unused", "--snapshot", "a", "--discarded", "b"}, "", 70},
	} {
		var out, diagnostic bytes.Buffer
		if code := run(tc.args, strings.NewReader(tc.input), &out, &diagnostic); code != tc.code {
			t.Fatalf("%v: %d want %d: %s", tc.args, code, tc.code, diagnostic.String())
		}
	}
	var out bytes.Buffer
	if code := run([]string{"--protocol"}, strings.NewReader(""), &out, io.Discard); code != 0 || out.String() != "qui-update-state-v1\n" {
		t.Fatal(code, out.String())
	}
}
