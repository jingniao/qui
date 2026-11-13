package subscription

import (
	"testing"

	"proxycontrol/internal/panel/subscription/format"
)

// v1.5.4 item 12: a link without ?format= picks the format by client. The
// strings are the User-Agents these clients send (case varies; matching
// ignores it). An unrecognised client gets the Owner default.
func TestDefaultRegistryPicksFormatByUserAgent(t *testing.T) {
	t.Parallel()
	registry := DefaultFormats()
	policy := format.Policy{Default: "uri", DetectUserAgent: true}
	for agent, want := range map[string]string{
		"clash-verge/v2.2.3":                                   "clash",
		"mihomo.party/v1.7.4 (clash.meta)":                     "clash",
		"ClashMetaForAndroid/2.11.7.Meta":                      "clash",
		"FlClash/v0.8.80 clash-verge Platform/android":         "clash",
		"mihomo/1.19.33":                                       "clash",
		"ClashX Meta/v1.4.6 (com.metacubex.ClashX.meta)":       "clash",
		"ClashforWindows/0.20.39":                              "clash-legacy",
		"ClashX/1.118.0 (com.west2online.ClashX; build:1.118)": "clash-legacy",
		"SFA/1.11.4 (582; sing-box 1.11.4; language zh_CN)":    "sing-box",
		"SFI/1.11.4 (Build 1; sing-box 1.11.4)":                "sing-box",
		"sing-box 1.14.3":                                      "sing-box",
		"v2rayN/7.10.5":                                        "base64",
		"v2rayNG/1.9.30":                                       "base64",
		"Shadowrocket/2070 CFNetwork/1568.100.1 Darwin/24.0.0": "base64",
		"curl/8.5.0":                                           "uri",
		"":                                                     "uri",
	} {
		exporter, _, err := registry.Resolve("", agent, policy)
		if err != nil || exporter.ID() != want {
			t.Errorf("%q: got %v %v, want %s", agent, exporter, err, want)
		}
	}
	// Detection off: everyone gets the default; ?format= always wins.
	if exporter, _, _ := registry.Resolve("", "clash-verge/v2.2.3", format.Policy{Default: "base64"}); exporter.ID() != "base64" {
		t.Fatal("detection off still matched the User-Agent:", exporter.ID())
	}
	if exporter, _, _ := registry.Resolve("sing-box", "clash-verge/v2.2.3", policy); exporter.ID() != "sing-box" {
		t.Fatal("explicit format lost to the User-Agent:", exporter.ID())
	}
}
