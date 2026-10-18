package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capturedLines reads a testdata file of lines captured from an official core.
// Comment and empty lines document the capture and are not fed to a parser.
func capturedLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// TestCapturedXrayAccessLinesParse pins the parser against unmodified access
// lines of the official Xray release: IPv4, bracketed IPv6, UDP, a rejection
// without a destination, a default route and a per-credential email.
func TestCapturedXrayAccessLinesParse(t *testing.T) {
	lines := capturedLines(t, "xray-access-lines.txt")
	expected := []XrayEvent{
		{Domain: "localhost", Port: 63157, Network: "tcp", InboundID: "entry", OutboundTag: "direct"},
		{TargetIP: "127.0.0.1", Port: 63157, Network: "tcp", InboundID: "entry", OutboundTag: "direct"},
		{TargetIP: "::1", Port: 63157, Network: "tcp", InboundID: "entry", OutboundTag: "direct"},
		{TargetIP: "127.0.0.1", Port: 63158, Network: "udp", InboundID: "entry", OutboundTag: "direct"},
		{Domain: "blocked.example", Port: 80, Network: "tcp", InboundID: "entry", OutboundTag: "block"},
		{TargetIP: "127.0.0.1", Port: 63157, Network: "tcp", InboundID: "open", OutboundTag: "direct"},
		{Rejected: true},
		{TargetIP: "127.0.0.1", Port: 63157, Network: "tcp", InboundID: "entry", OutboundTag: "direct"},
		{Domain: "localhost", Port: 63157, Network: "tcp", InboundID: "vless-entry", OutboundTag: "direct", AccountID: "cred-1"},
	}
	if len(lines) != len(expected) {
		t.Fatalf("captured fixture changed: %d lines, %d expectations", len(lines), len(expected))
	}
	for index, line := range lines {
		event, err := ParseXrayAccessLine(line)
		if err != nil {
			t.Fatalf("captured line %d rejected: %v", index, err)
		}
		if event != expected[index] {
			t.Fatalf("captured line %d parsed as %+v, want %+v", index, event, expected[index])
		}
	}
}
