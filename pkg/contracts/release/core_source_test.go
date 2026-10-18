package release

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

const sampleDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCoreAssetURLAcceptsOfficialPrefixesOnly(t *testing.T) {
	accepted := []struct{ component, value string }{
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"},
		{"sing-box", "https://github.com/SagerNet/sing-box/releases/download/v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz"},
		{"sing-box", "https://github.com/SagerNet/sing-box/releases/download/v1.14.3/sing-box-1.14.3-windows-amd64.zip"},
	}
	for _, item := range accepted {
		u, tag, err := CoreAssetURL(item.component, item.value)
		if err != nil || u == nil || tag == "" {
			t.Fatalf("official asset rejected: %s: %v", item.value, err)
		}
	}
	rejected := []struct{ component, value string }{
		{"xray", "http://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"},
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.tar.gz"},
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip?x=1"},
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip#frag"},
		{"xray", "https://user:pass@github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"},
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/../Xray-linux-64.zip"},
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/sub/Xray-linux-64.zip"},
		{"xray", "https://github.com/xtls/xray-core/releases/download/v26.3.27/Xray-linux-64.zip"},
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/"},
		{"xray", "https://evil.example.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"},
		{"xray", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/xray"},
		{"sing-box", "https://github.com/SagerNet/sing-box/releases/download/v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz/extra"},
		{"sing-box", "https://github.com/XTLS/Xray-core/releases/download/v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz"},
		{"proxy-agent", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"},
	}
	for _, item := range rejected {
		if _, _, err := CoreAssetURL(item.component, item.value); err == nil {
			t.Fatalf("asset accepted: %s", item.value)
		}
	}
}

func TestCoreRedirectHostAllowList(t *testing.T) {
	for _, host := range []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"} {
		if !CoreRedirectHost(host) {
			t.Fatalf("allowed redirect host rejected: %s", host)
		}
	}
	for _, host := range []string{"", "evil.example.com", "github.com.evil.example.com", "githubusercontent.com", "raw.githubusercontent.com", "objects.githubusercontent.com.evil.example.com"} {
		if CoreRedirectHost(host) {
			t.Fatalf("redirect host accepted: %s", host)
		}
	}
}

func TestCoreBinaryName(t *testing.T) {
	cases := []struct{ component, goos, want string }{
		{"xray", "linux", "xray"},
		{"xray", "windows", "xray.exe"},
		{"sing-box", "linux", "sing-box"},
		{"sing-box", "windows", "sing-box.exe"},
	}
	for _, item := range cases {
		if got := CoreBinaryName(item.component, item.goos); got != item.want {
			t.Fatalf("CoreBinaryName(%q,%q)=%q want %q", item.component, item.goos, got, item.want)
		}
	}
}

func TestCoreAssetMatchesPlatformAdvisory(t *testing.T) {
	cases := []struct {
		component, osName, architecture, asset string
		want                                   bool
	}{
		{"xray", "linux", "amd64", "Xray-linux-64.zip", true},
		{"xray", "linux", "arm64", "Xray-linux-arm64-v8a.zip", true},
		{"xray", "windows", "amd64", "Xray-windows-64.zip", true},
		{"xray", "linux", "amd64", "Xray-linux-arm64-v8a.zip", false},
		{"xray", "linux", "arm64", "Xray-linux-64.zip", false},
		{"xray", "windows", "amd64", "Xray-linux-64.zip", false},
		{"xray", "linux", "amd64", "Xray-linux-64.tar.gz", false},
		{"sing-box", "linux", "amd64", "sing-box-1.14.3-linux-amd64.tar.gz", true},
		{"sing-box", "windows", "amd64", "sing-box-1.14.3-windows-amd64.zip", true},
		{"sing-box", "linux", "arm64", "sing-box-1.14.3-linux-arm64.tar.gz", true},
		{"sing-box", "linux", "amd64", "sing-box-1.14.3-linux-arm64.tar.gz", false},
		{"sing-box", "linux", "amd64", "sing-box-1.14.3-windows-amd64.zip", false},
		{"sing-box", "linux", "amd64", "sing-box-1.14.3-linux-amd64", false},
	}
	for _, item := range cases {
		if got := CoreAssetMatches(item.component, item.osName, item.architecture, item.asset); got != item.want {
			t.Fatalf("CoreAssetMatches(%q,%q,%q,%q)=%v want %v", item.component, item.osName, item.architecture, item.asset, got, item.want)
		}
	}
}

func TestCoreUpdateRequestValidation(t *testing.T) {
	valid := CoreUpdateRequest{SchemaVersion: 2, ID: "upd_abc", NodeID: "node", Revision: 3, Sequence: 1, Component: "xray", Version: "26.3.27", URL: "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip", SHA256: sampleDigest}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeCoreUpdateRequest(data); err != nil || decoded != valid {
		t.Fatalf("round trip failed: %v", err)
	}
	unknown := strings.TrimSuffix(string(data), "}") + `,"extra":1}`
	if _, err := DecodeCoreUpdateRequest([]byte(unknown)); err == nil {
		t.Fatal("unknown field accepted")
	}
	broken := []CoreUpdateRequest{
		{SchemaVersion: 1, ID: "upd_abc", NodeID: "node", Revision: 3, Sequence: 1, Component: "xray", Version: "26.3.27", URL: valid.URL, SHA256: sampleDigest},
		{SchemaVersion: 2, ID: "bad id", NodeID: "node", Revision: 3, Sequence: 1, Component: "xray", Version: "26.3.27", URL: valid.URL, SHA256: sampleDigest},
		{SchemaVersion: 2, ID: "upd_abc", NodeID: "node", Revision: 0, Sequence: 1, Component: "xray", Version: "26.3.27", URL: valid.URL, SHA256: sampleDigest},
		{SchemaVersion: 2, ID: "upd_abc", NodeID: "node", Revision: 3, Sequence: 0, Component: "xray", Version: "26.3.27", URL: valid.URL, SHA256: sampleDigest},
		{SchemaVersion: 2, ID: "upd_abc", NodeID: "node", Revision: 3, Sequence: 1, Component: "proxy-agent", Version: "26.3.27", URL: valid.URL, SHA256: sampleDigest},
		{SchemaVersion: 2, ID: "upd_abc", NodeID: "node", Revision: 3, Sequence: 1, Component: "xray", Version: "26.3.27-beta.1", URL: valid.URL, SHA256: sampleDigest},
		{SchemaVersion: 2, ID: "upd_abc", NodeID: "node", Revision: 3, Sequence: 1, Component: "xray", Version: "26.3.27", URL: valid.URL, SHA256: strings.ToUpper(sampleDigest)},
		{SchemaVersion: 2, ID: "upd_abc", NodeID: "node", Revision: 3, Sequence: 1, Component: "xray", Version: "26.3.27", URL: "https://example.com/x.zip", SHA256: sampleDigest},
	}
	for index, request := range broken {
		if err := request.Validate(); err == nil {
			t.Fatalf("invalid request %d accepted", index)
		}
	}
}

func TestCoreUpdateResultValidation(t *testing.T) {
	installed := strings.Repeat("ab", 32)
	if _, err := hex.DecodeString(installed); err != nil {
		t.Fatal(err)
	}
	valid := UpdateResult{ID: "upd_abc", NodeID: "node", Revision: 3, TargetDigest: sampleDigest, Status: "success", Code: "success", InstalledDigest: installed, InstalledVersion: "26.3.27", InstalledSequence: 1}
	if err := valid.ValidateCore(); err != nil {
		t.Fatalf("valid core result rejected: %v", err)
	}
	if err := valid.Validate(); err == nil {
		t.Fatal("signed-path validation accepted a different installed digest")
	}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCoreUpdateResult(data); err != nil {
		t.Fatalf("core result decode failed: %v", err)
	}
	noDigest := UpdateResult{ID: "upd_abc", NodeID: "node", Revision: 3, TargetDigest: sampleDigest, Status: "success", Code: "success", InstalledVersion: "26.3.27", InstalledSequence: 1}
	if err := noDigest.ValidateCore(); err == nil {
		t.Fatal("success without an installed digest accepted")
	}
	agentCode := UpdateResult{ID: "upd_abc", NodeID: "node", Revision: 3, TargetDigest: sampleDigest, Status: "rejected", Code: "trust_failed"}
	if err := agentCode.ValidateCore(); err == nil {
		t.Fatal("Agent-only result code accepted for a core")
	}
	checksum := UpdateResult{ID: "upd_abc", NodeID: "node", Revision: 3, TargetDigest: sampleDigest, Status: "rejected", Code: "checksum_mismatch"}
	if err := checksum.ValidateCore(); err != nil {
		t.Fatalf("checksum rejection refused: %v", err)
	}
	failed := UpdateResult{ID: "upd_abc", NodeID: "node", Revision: 3, TargetDigest: sampleDigest, Status: "failed_closed", Code: "installation_failed", InstalledDigest: installed, InstalledVersion: "26.3.27", InstalledSequence: 1}
	if err := failed.ValidateCore(); err != nil {
		t.Fatalf("failed_closed result refused: %v", err)
	}
}
