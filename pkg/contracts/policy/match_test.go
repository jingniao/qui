package policy

import (
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
	"time"
)

func rule() desiredstate.Policy {
	return desiredstate.Policy{ID: "rule-1", Name: "Test policy", Priority: 10, Enabled: true, Action: desiredstate.Action{Type: "Block"}, Match: desiredstate.Match{Suffixes: []string{"example.com"}}}
}

func TestTimeWindowsUsePolicyZoneAndSupportMidnight(t *testing.T) {
	policy := rule()
	policy.Timezone = "Asia/Shanghai"
	policy.Match.TimeRange = "23:00-06:00"
	if err := Validate(&policy); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		hour, minute int
		want         bool
	}{{14, 59, false}, {15, 0, true}, {20, 0, true}, {21, 59, true}, {22, 0, false}} {
		at := time.Date(2026, 10, 1, test.hour, test.minute, 0, 0, time.UTC)
		if got := TimeActive(policy, at); got != test.want {
			t.Fatalf("%v active=%v want=%v", at, got, test.want)
		}
	}
	policy.Timezone = "America/Los_Angeles"
	policy.Match.TimeRange = "09:00-10:00"
	if !TimeActive(policy, time.Date(2026, 7, 1, 16, 30, 0, 0, time.UTC)) || !TimeActive(policy, time.Date(2026, 1, 1, 17, 30, 0, 0, time.UTC)) {
		t.Fatal("daylight saving offset ignored")
	}
	for _, value := range []string{"24:00-25:00", "09:00-09:00", "9:00-10:00", "10:60-11:00"} {
		policy.Match.TimeRange = value
		if Validate(&policy) == nil {
			t.Fatal("invalid time window accepted", value)
		}
	}
}

func TestKeywordValidation(t *testing.T) {
	cases := []struct {
		name    string
		keyword string
		wantErr bool
	}{
		{"uppercase rejected", "OPENAI", true},
		{"comma rejected", "open,ai", true},
		{"space rejected", "open ai", true},
		{"too long rejected", strings.Repeat("a", 254), true},
		{"valid lowercase", "openai", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := desiredstate.Policy{
				ID:       "test",
				Name:     "Test",
				Priority: 10,
				Action:   desiredstate.Action{Type: "Direct"},
				Match:    desiredstate.Match{Keywords: []string{tc.keyword}},
			}
			err := Validate(&p)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate(%q) err = %v, wantErr = %v", tc.keyword, err, tc.wantErr)
			}
		})
	}
}

func TestDomainsWildcardIDNAAndCIDRs(t *testing.T) {
	policy := rule()
	policy.Match = desiredstate.Match{Domains: []string{"*.Example.COM", "EXACT.test."}, CIDRs: []string{"2001:db8::/32"}}
	if err := Validate(&policy); err != nil {
		t.Fatal(err)
	}
	domain, err := Domain("例子.测试", false)
	if err != nil || domain != "xn--fsqu00a.xn--0zwm56d" {
		t.Fatal("IDNA normalization failed", domain, err)
	}
	for _, value := range []string{"https://example.com/path", "example.com?password=value", "user@example.com", "bad domain.com", "example.com:443", "regexp:.*"} {
		if _, err := Domain(value, true); err == nil {
			t.Fatal("URL or raw regexp accepted", value)
		}
	}
}
