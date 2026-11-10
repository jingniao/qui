package tuning

import (
	"slices"
	"strconv"
	"testing"
)

func TestWhitelistKeysAndBounds(t *testing.T) {
	keys := Keys()
	if len(keys) != 19 {
		t.Fatalf("got %d keys (%v), want 19", len(keys), keys)
	}
	if !slices.IsSorted(keys) {
		t.Fatalf("keys not sorted: %v", keys)
	}

	newBounds := map[string]Bound{
		"net.ipv4.tcp_slow_start_after_idle": {Min: 0, Max: 1},
		"net.ipv4.tcp_notsent_lowat":         {Min: 16384, Max: 16777216},
		"net.core.rmem_default":              {Min: 212992, Max: 268435456},
		"net.core.wmem_default":              {Min: 212992, Max: 268435456},
		"net.ipv4.udp_rmem_min":              {Min: 4096, Max: 1048576},
		"net.ipv4.udp_wmem_min":              {Min: 4096, Max: 1048576},
	}
	for k, want := range newBounds {
		got, ok := Bounds[k]
		if !ok || got != want {
			t.Fatalf("Bounds[%s] = %v (ok=%v), want %v", k, got, ok, want)
		}
		if err := ValidateValue(k, strconv.FormatUint(want.Min, 10)); err != nil {
			t.Fatalf("ValidateValue(%s, min) failed: %v", k, err)
		}
		if err := ValidateValue(k, strconv.FormatUint(want.Max, 10)); err != nil {
			t.Fatalf("ValidateValue(%s, max) failed: %v", k, err)
		}
		if want.Min > 0 {
			if err := ValidateValue(k, strconv.FormatUint(want.Min-1, 10)); err == nil {
				t.Fatalf("ValidateValue(%s, min-1) succeeded", k)
			}
		} else {
			if err := ValidateValue(k, "-1"); err == nil {
				t.Fatalf("ValidateValue(%s, -1) succeeded", k)
			}
		}
		if err := ValidateValue(k, strconv.FormatUint(want.Max+1, 10)); err == nil {
			t.Fatalf("ValidateValue(%s, max+1) succeeded", k)
		}
	}

	if !Vector("net.ipv4.tcp_rmem") || !Vector("net.ipv4.tcp_wmem") || Vector("net.core.rmem_max") {
		t.Fatal("unexpected Vector result")
	}

	for _, bad := range [][2]string{
		{"kernel.core_pattern", "1"},
		{"net.ipv4.tcp_congestion_control", "unknown_cc"},
		{"net.ipv4.tcp_congestion_control", "bbr "},
		{"net.core.default_qdisc", "fq\n"},
		{"net.core.somaxconn", "+128"},
		{"net.core.somaxconn", "0128"},
	} {
		if err := ValidateValue(bad[0], bad[1]); err == nil {
			t.Fatalf("ValidateValue(%q, %q) succeeded", bad[0], bad[1])
		}
	}

	for k, list := range Choices {
		for _, v := range list {
			if err := ValidateValue(k, v); err != nil {
				t.Fatalf("ValidateValue(%q, %q) failed: %v", k, v, err)
			}
		}
	}
}
