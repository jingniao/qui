package importer

import (
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
)

func TestSalamanderImportExports(t *testing.T) {
	for _, input := range []string{
		"hysteria2://secret@example.com:443?obfs=salamander&obfs-password=private-obfs",
		"proxies:\n- {name: hy, type: hysteria2, server: example.com, port: 443, password: secret, obfs: salamander, obfs-password: private-obfs}",
	} {
		_, entries := Analyze([]byte(input))
		if len(entries) != 1 || entries[0].QUIC.Obfs != "salamander" || entries[0].QUIC.ObfsPassword != "private-obfs" {
			t.Fatal("missing obfuscation")
		}
		for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}} {
			out, err := exporter.Render(format.Profile{Proxies: entries})
			if err != nil || !strings.Contains(string(out.Body), "salamander") || !strings.Contains(string(out.Body), "private-obfs") {
				t.Fatalf("export %s: %v", exporter.ID(), err)
			}
		}
		link, err := uri.Link(entries[0])
		if err != nil || !strings.Contains(link, "obfs=salamander") || !strings.Contains(link, "obfs-password=private-obfs") {
			t.Fatal("URI export failed", err)
		}
	}
}

func TestSalamanderRejectedDiagnostics(t *testing.T) {
	for _, query := range []string{"obfs=xplus&obfs-password=private-obfs", "obfs=salamander", "obfs-password=private-obfs", "obfs=salamander&obfs-password=" + strings.Repeat("x", 257), "obfs=salamander&obfs-password=private-obfs%0A"} {
		result, entries := Analyze([]byte("hysteria2://secret@example.com:443?" + query))
		if len(entries) != 0 {
			t.Fatal("accepted invalid obfuscation")
		}
		raw, err := json.Marshal(result)
		if err != nil || strings.Contains(string(raw), "private-obfs") {
			t.Fatal("unsafe diagnostic", err)
		}
	}
	for _, input := range []string{
		"tuic://6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef:secret@example.com:443?obfs=salamander&obfs-password=private-obfs",
		"proxies:\n- {name: hy, type: tuic, server: example.com, port: 443, password: secret, obfs: salamander, obfs-password: private-obfs}",
	} {
		_, entries := Analyze([]byte(input))
		if len(entries) != 0 {
			t.Fatal("accepted TUIC obfuscation")
		}
	}
}
