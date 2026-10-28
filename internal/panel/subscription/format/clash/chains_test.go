package clash

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
)

func chainProfile() format.Profile {
	return format.Profile{Proxies: []proxy.Proxy{
		{Name: "DIRECT", Protocol: "socks5", Server: "a.example.com", Port: 1080, ClientChain: "fixture"},
		{Name: "exit", Protocol: "socks5", Server: "b.example.com", Port: 1080, DialerProxy: "DIRECT", ClientChain: "fixture"},
	}}
}
func TestClientChainRenamesDependenciesAndDropsUnsupportedFormats(t *testing.T) {
	out, err := (Meta{}).Render(chainProfile())
	if err != nil || !strings.Contains(string(out.Body), "dialer-proxy: DIRECT (2)") {
		t.Fatal("dialer name did not follow collision renaming")
	}
	for _, exporter := range []format.Exporter{Legacy{}, uri.URI{}, uri.Base64{}, singbox.Exporter{}} {
		out, err = exporter.Render(chainProfile())
		if err != nil || len(out.Warnings) == 0 || strings.Contains(string(out.Body), "b.example.com") || strings.Contains(string(out.Body), "a.example.com") {
			t.Fatalf("unsupported chain retained by %s", exporter.ID())
		}
	}
	p := chainProfile()
	p.Proxies[0].Protocol = "unsupported"
	out, err = (Meta{}).Render(p)
	if err != nil || strings.Contains(string(out.Body), "b.example.com") || len(out.Warnings) < 2 {
		t.Fatal("orphaned chain was retained")
	}
}

// AU5.2 is intentionally env-gated. Never run a downloaded core on the local
// workstation; CI uses the existing digest-verified mihomo and seeded home.
func TestMihomoAcceptsClientDialerProxyChain(t *testing.T) {
	binary, home := os.Getenv("MIHOMO_BINARY"), os.Getenv("MIHOMO_HOME")
	if binary == "" || home == "" {
		t.Skip("awaiting Owner/CI run: MIHOMO_BINARY and MIHOMO_HOME are not set")
	}
	out, err := (Meta{}).Render(chainProfile())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "client-chain.yaml")
	if err = os.WriteFile(path, out.Body, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(binary, "-t", "-d", home, "-f", path).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "test is successful") {
		t.Fatalf("mihomo chain validation failed: %v", err)
	}
}
