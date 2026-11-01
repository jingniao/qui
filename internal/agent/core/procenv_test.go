package core

import (
	"slices"
	"testing"
)

func TestCoreEnvironmentDisablesMPTCPAndKeepsTheRest(t *testing.T) {
	cases := map[string]struct{ parent, want []string }{
		"no GODEBUG":       {[]string{"PATH=/bin"}, []string{"PATH=/bin", "GODEBUG=multipathtcp=0"}},
		"other settings":   {[]string{"GODEBUG=http2client=0,x509sha1=1", "HOME=/"}, []string{"HOME=/", "GODEBUG=multipathtcp=0,http2client=0,x509sha1=1"}},
		"MPTCP turned on":  {[]string{"GODEBUG=multipathtcp=1,netdns=go"}, []string{"GODEBUG=multipathtcp=0,netdns=go"}},
		"empty GODEBUG":    {[]string{"GODEBUG="}, []string{"GODEBUG=multipathtcp=0"}},
		"similar variable": {[]string{"GODEBUGX=1"}, []string{"GODEBUGX=1", "GODEBUG=multipathtcp=0"}},
	}
	for name, c := range cases {
		if got := coreEnvironment(c.parent); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}
