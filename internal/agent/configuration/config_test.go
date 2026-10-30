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
		{"cores: []\n", "", true},
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
			if len(cores) > 1 && (c.CoreName != cores[0].Name || c.CorePath != cores[0].Path) {
				t.Fatal("primary compatibility fields differ")
			}
		})
	}
}
