package release

import (
	"strings"
	"testing"
)

func TestQuiCoreAssetURLExactShape(t *testing.T) {
	const prefix = "https://github.com/jingniao/qui/releases/download/"
	const asset = "sing-box-1.14.4-qui.1-linux-amd64.tar.gz"
	valid := prefix + "v1.1.0/" + asset
	for _, arch := range []string{"amd64", "arm64"} {
		value := strings.Replace(valid, "amd64", arch, 1)
		if _, _, err := CoreAssetURL("sing-box", value); err != nil {
			t.Fatalf("valid qui asset rejected: %v", err)
		}
	}
	for _, value := range []string{
		strings.Replace(valid, "jingniao", "other", 1),
		strings.Replace(valid, "/qui/", "/other/", 1),
		valid + "/extra", valid + "?x=1", valid + "?", valid + "#fragment",
		strings.TrimSuffix(valid, ".tar.gz") + ".zip",
		prefix + "../" + asset, prefix + "v1.1.0/../" + asset,
		strings.Replace(valid, "v1.1.0", "v1..0", 1),
		strings.Replace(valid, "qui.1", "qui.0", 1),
		strings.Replace(valid, "amd64", "amd64-musl", 1),
		strings.Replace(valid, "amd64", "386", 1),
		strings.Replace(valid, "sing-box", "%73ing-box", 1),
		strings.Replace(valid, "v1.1.0", "%2e%2e", 1),
		strings.Replace(valid, "v1.1.0", "v1.1.0%2fextra", 1),
		strings.Replace(valid, "v1.1.0", "%252e%252e", 1),
		strings.Replace(valid, "github.com/", "github.com.evil/", 1),
	} {
		if _, _, err := CoreAssetURL("sing-box", value); err == nil {
			t.Errorf("lookalike accepted: %s", value)
		}
	}
	if _, _, err := CoreAssetURL("xray", valid); err == nil {
		t.Fatal("qui source accepted for Xray")
	}
}
