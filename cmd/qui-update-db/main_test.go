package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunArguments(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		status int
		output string
	}{
		{[]string{"--protocol"}, 0, "qui-update-database-v1\n"},
		{[]string{"--version"}, 0, "qui-update-db " + version + "\n"},
		{nil, 70, ""}, {[]string{"unknown"}, 70, ""}, {[]string{"compare"}, 2, ""}, {[]string{"copy"}, 1, ""}, {[]string{"preflight"}, 70, ""},
		{[]string{"preflight", "--account", "root"}, 70, ""},
	} {
		var out, diagnostic bytes.Buffer
		code := run(context.Background(), tc.args, &out, &diagnostic)
		if code != tc.status || out.String() != tc.output {
			t.Fatalf("%v: code %d out %q err %q", tc.args, code, out.String(), diagnostic.String())
		}
		if code != 0 && strings.TrimSpace(diagnostic.String()) == "" {
			t.Fatalf("missing error for %v", tc.args)
		}
	}
}
