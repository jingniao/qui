package policy_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/panel/policy"
)

func TestParseClassicalListTolerantFormat(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "openai.list"))
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}

	entries, skipped, err := policy.ParseRuleList(data, "text", "classical")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries.Domains) != 2 {
		t.Errorf("expected 2 domains, got %d: %v", len(entries.Domains), entries.Domains)
	}
	if len(entries.Keywords) != 2 {
		t.Errorf("expected 2 keywords, got %d: %v", len(entries.Keywords), entries.Keywords)
	}
	if len(entries.Suffixes) != 4 {
		t.Errorf("expected 4 suffixes, got %d: %v", len(entries.Suffixes), entries.Suffixes)
	}
	if len(entries.CIDRs) != 2 {
		t.Errorf("expected 2 cidrs, got %d: %v", len(entries.CIDRs), entries.CIDRs)
	}
	if skipped != 2 { // PROCESS-NAME and USER-AGENT
		t.Errorf("expected 2 skipped entries, got %d", skipped)
	}

	// Test copy with UTF-8 BOM and CRLF
	crlfData := bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	bomData := append([]byte("\xef\xbb\xbf"), crlfData...)

	bomEntries, bomSkipped, err := policy.ParseRuleList(bomData, "text", "classical")
	if err != nil {
		t.Fatalf("unexpected error on BOM/CRLF data: %v", err)
	}
	if len(bomEntries.Domains) != len(entries.Domains) ||
		len(bomEntries.Keywords) != len(entries.Keywords) ||
		len(bomEntries.Suffixes) != len(entries.Suffixes) ||
		len(bomEntries.CIDRs) != len(entries.CIDRs) ||
		bomSkipped != skipped {
		t.Errorf("BOM/CRLF parsed result differed from raw result")
	}
}

func TestParseDomainBehavior(t *testing.T) {
	text := `
# Domain behavior list
+.openai.com
.chatgpt.com
api.openai.com
// comment line
invalid..domain
`
	entries, skipped, err := policy.ParseRuleList([]byte(text), "text", "domain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries.Suffixes) != 2 {
		t.Errorf("expected 2 suffixes, got %d: %v", len(entries.Suffixes), entries.Suffixes)
	}
	if len(entries.Domains) != 1 {
		t.Errorf("expected 1 domain, got %d: %v", len(entries.Domains), entries.Domains)
	}
	if skipped != 1 { // invalid..domain
		t.Errorf("expected 1 skipped entry, got %d", skipped)
	}
}

func TestParseIPCIDRBehavior(t *testing.T) {
	text := `
1.1.1.1/32
10.0.0.0/8
2606:4700::/32
192.168.1.1
not-an-ip
`
	entries, skipped, err := policy.ParseRuleList([]byte(text), "text", "ipcidr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries.CIDRs) != 4 {
		t.Errorf("expected 4 CIDRs, got %d: %v", len(entries.CIDRs), entries.CIDRs)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
}

func TestParseYAMLPayload(t *testing.T) {
	yamlContent := `
payload:
  - DOMAIN-SUFFIX,openai.com
  - DOMAIN,cdn.oaistatic.com
  - DOMAIN-KEYWORD,openai
  - IP-CIDR,10.0.0.0/8
  - USER-AGENT,some-agent
`
	entries, skipped, err := policy.ParseRuleList([]byte(yamlContent), "yaml", "classical")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries.Suffixes) != 1 || len(entries.Domains) != 1 || len(entries.Keywords) != 1 || len(entries.CIDRs) != 1 {
		t.Errorf("unexpected parsed entries: %+v", entries)
	}
	if skipped != 1 { // USER-AGENT
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
}

func TestParseCapsEntries(t *testing.T) {
	var builder strings.Builder
	for i := 1; i <= 30000; i++ {
		builder.WriteString(fmt.Sprintf("DOMAIN,test-%05d.example.com\n", i))
	}

	entries, skipped, err := policy.ParseRuleList([]byte(builder.String()), "text", "classical")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries.Domains) != policy.MaxSourceEntries {
		t.Errorf("expected %d entries, got %d", policy.MaxSourceEntries, len(entries.Domains))
	}
	if skipped != 10000 {
		t.Errorf("expected 10000 skipped, got %d", skipped)
	}
}

func TestParseMRSIsClientOnly(t *testing.T) {
	_, _, err := policy.ParseRuleList([]byte("some-mrs-data"), "mrs", "domain")
	if err != policy.ErrNotParsable {
		t.Fatalf("expected ErrNotParsable, got %v", err)
	}
}

func TestParseEmptyIsError(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>404 Not Found</title></head><body><h1>404 Not Found</h1></body></html>`
	_, _, err := policy.ParseRuleList([]byte(html), "text", "classical")
	if err != policy.ErrEmptyList {
		t.Fatalf("expected ErrEmptyList, got %v", err)
	}
}
