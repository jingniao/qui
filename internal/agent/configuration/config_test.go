package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreConfigMapping(t *testing.T) {
	for _, tc := range []struct {
		yaml    string
		names   string
		invalid bool
	}{
		{"core_name: sing-box\ncore_path: /bin/sing-box\n", "sing-box", false},
		{"cores:\n - name: xray\n   path: /bin/xray\n - name: sing-box\n   path: /bin/sing-box\n", "xray,sing-box", false},
		{"cores:\n - name: gost\n   path: /usr/local/lib/proxy-agent/gost\n", "gost", false},
		{"cores:\n - name: gost\n   path: /usr/local/lib/proxy-agent/gost\n - name: xray\n   path: /bin/xray\n", "gost,xray", false},
		{"cores:\n - name: sing-box\n   path: /bin/sing-box\n - name: xray\n   path: /bin/xray\n - name: gost\n   path: /usr/local/lib/proxy-agent/gost\n", "sing-box,xray,gost", false},
		{"cores:\n - name: xray\n   path: /bin/xray\n - name: sing-box\n   path: /bin/sing-box\n - name: gost\n   path: /usr/local/lib/proxy-agent/gost\n - name: gost\n   path: /usr/local/lib/proxy-agent/other\n", "", true},
		{"cores: []\n", "", false},
		{"# no cores field\n", "xray", false},
		{"cores:\n - name: xray\n   path: /bin/xray\n - name: xray\n   path: /bin/other\n", "", true},
		{"cores:\n - name: shell\n   path: /bin/sh\n", "", true},
		{"cores:\n - name: xray\n   path: /bin/xray\n   args: test\n", "", true},
	} {
		t.Run(tc.names+tc.yaml[:5], func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			// Development mode does not require loading production TLS material.
			if err := os.WriteFile(p, []byte("development: true\ntls:\n client_ca: ''\n"+tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := LoadAgent(p)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid config accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cores := Cores(c)
			names := []string{}
			for _, core := range cores {
				names = append(names, core.Name)
			}
			if strings.Join(names, ",") != tc.names {
				t.Fatal(cores)
			}
			primary := PrimaryCore(cores)
			if tc.yaml != "# no cores field\n" && (c.CoreName != primary.Name || c.CorePath != primary.Path) {
				t.Fatal("primary compatibility fields differ")
			}
		})
	}
}

// GOST may lead the list, while the first user core owns the scalar fields.
func TestFirstUserCoreBecomesPrimary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := "development: true\ntls:\n client_ca: ''\ncores:\n - name: gost\n   path: /usr/local/lib/proxy-agent/gost\n - name: sing-box\n   path: /bin/sing-box\n"
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadAgent(p)
	if err != nil || c.CoreName != "sing-box" || c.CorePath != "/bin/sing-box" {
		t.Fatal(c, err)
	}
}

// The legacy scalar core_name follows the same rule as the cores list.
func TestLegacyGostCoreNameIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("development: true\ntls:\n client_ca: ''\ncore_name: gost\ncore_path: /usr/local/lib/proxy-agent/gost\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgent(p); err != errGostPrimary {
		t.Fatal(err)
	}
}
