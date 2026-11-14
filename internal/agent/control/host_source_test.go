package control

import (
	"proxycontrol/internal/agent/hostnet"
	"testing"
)

func TestHostSourceExtraExemptSupported(t *testing.T) {
	for _, tc := range []struct {
		name              string
		installed, status bool
		features          []string
		want              bool
	}{
		{"marked", true, true, []string{hostnet.FeatureExtraExempt}, true},
		{"unmarked", true, true, nil, false},
		{"missing status", true, false, nil, false},
		{"missing helper", false, true, []string{hostnet.FeatureExtraExempt}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.status {
				if err := hostnet.WriteStatus(dir, hostnet.Status{Features: tc.features}); err != nil {
					t.Fatal(err)
				}
			}
			if got := hostSourceExtraExemptSupported(dir, tc.installed); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
