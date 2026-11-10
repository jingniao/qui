package tuningadvice

import (
	"proxycontrol/pkg/contracts/tuning"
	"strings"
	"testing"
)

func allWritableKeys() map[string]KeyState {
	m := make(map[string]KeyState, len(tuning.Keys()))
	for _, k := range tuning.Keys() {
		m[k] = KeyState{Present: true, Writable: true}
	}
	return m
}

func lxcKeys() map[string]KeyState {
	m := allWritableKeys()
	for k := range m {
		if strings.HasPrefix(k, "net.core.") {
			m[k] = KeyState{Present: false, Writable: false}
		}
	}
	return m
}

func findAdvice(items []Advice, key string) (Advice, bool) {
	for _, item := range items {
		if item.Key == key {
			return item, true
		}
	}
	return Advice{}, false
}

func TestAdviceSkipsAbsentKeys(t *testing.T) {
	in := Inputs{
		MemoryBytes:         330 * 1024 * 1024,
		CPUCores:            1,
		Virtualization:      "lxc",
		Keys:                lxcKeys(),
		Current:             map[string]string{"net.ipv4.tcp_congestion_control": "cubic", "net.ipv4.tcp_slow_start_after_idle": "1", "net.ipv4.tcp_notsent_lowat": "4294967295", "net.ipv4.tcp_rmem": "4096 131072 6291456", "net.ipv4.tcp_wmem": "4096 16384 4194304"},
		CongestionAvailable: []string{"cubic", "reno"},
		QdiscAvailable:      []string{"fq", "pfifo_fast"},
		HasQUICInbound:      true,
	}
	got := Recommend(in)
	for _, a := range got {
		if strings.HasPrefix(a.Key, "net.core.") {
			t.Fatalf("unexpected net.core.* advice on LXC: %+v", a)
		}
	}
}

func TestAdviceSkipsSatisfiedKeys(t *testing.T) {
	in := Inputs{
		MemoryBytes:         2 * 1024 * 1024 * 1024,
		Keys:                allWritableKeys(),
		CongestionAvailable: []string{"bbr", "cubic"},
		QdiscAvailable:      []string{"fq", "fq_codel"},
		Current: map[string]string{
			"net.ipv4.tcp_congestion_control":    "bbr",
			"net.core.default_qdisc":             "fq",
			"net.ipv4.tcp_slow_start_after_idle": "0",
			"net.ipv4.tcp_notsent_lowat":         "131072",
			"net.ipv4.tcp_rmem":                  "4096 131072 33554432",
			"net.ipv4.tcp_wmem":                  "4096 16384 33554432",
			"net.core.rmem_max":                  "33554432",
			"net.core.wmem_max":                  "33554432",
		},
	}
	got := Recommend(in)
	if len(got) != 0 {
		t.Fatalf("expected no advice when all keys satisfied, got %+v", got)
	}
}

func TestBBRLoadableIsAdvised(t *testing.T) {
	in := Inputs{
		MemoryBytes:         1024 * 1024 * 1024,
		Keys:                allWritableKeys(),
		CongestionAvailable: []string{"reno", "cubic"},
		CongestionLoadable:  []string{"bbr"},
		QdiscAvailable:      []string{"pfifo_fast"},
		QdiscLoadable:       []string{"fq"},
		Current: map[string]string{
			"net.ipv4.tcp_congestion_control": "cubic",
			"net.core.default_qdisc":          "pfifo_fast",
		},
	}
	got := Recommend(in)
	if cc, ok := findAdvice(got, "net.ipv4.tcp_congestion_control"); !ok || cc.Value != "bbr" || cc.Rule != "cc-bbr" {
		t.Fatalf("expected cc-bbr advice, got %+v (ok=%v)", cc, ok)
	}
	if qd, ok := findAdvice(got, "net.core.default_qdisc"); !ok || qd.Value != "fq" || qd.Rule != "qdisc-fq" {
		t.Fatalf("expected qdisc-fq advice, got %+v (ok=%v)", qd, ok)
	}
}

func TestNoBBRWithoutModule(t *testing.T) {
	in := Inputs{
		MemoryBytes:         1024 * 1024 * 1024,
		Keys:                allWritableKeys(),
		CongestionAvailable: []string{"reno", "cubic"},
		CongestionLoadable:  nil,
		Current: map[string]string{
			"net.ipv4.tcp_congestion_control": "cubic",
		},
	}
	got := Recommend(in)
	if _, ok := findAdvice(got, "net.ipv4.tcp_congestion_control"); ok {
		t.Fatalf("expected no bbr advice when neither available nor loadable")
	}
}

func TestBufferTiers(t *testing.T) {
	cases := []struct {
		name string
		mem  uint64
		want string
	}{
		{"330MB", 330 * 1024 * 1024, "8388608"},
		{"930MB", 930 * 1024 * 1024, "8388608"},
		{"1.5GiB", 1536 * 1024 * 1024, "16777216"},
		{"2GiB", 2 * 1024 * 1024 * 1024, "33554432"},
	}
	for _, tc := range cases {
		in := Inputs{
			MemoryBytes: tc.mem,
			Keys:        allWritableKeys(),
			Current: map[string]string{
				"net.core.rmem_max": "212992",
				"net.core.wmem_max": "212992",
				"net.ipv4.tcp_rmem": "4096 131072 4194304",
				"net.ipv4.tcp_wmem": "4096 16384 4194304",
			},
		}
		got := Recommend(in)
		for _, k := range []string{"net.core.rmem_max", "net.core.wmem_max", "net.ipv4.tcp_rmem", "net.ipv4.tcp_wmem"} {
			a, ok := findAdvice(got, k)
			if !ok || a.Value != tc.want {
				t.Fatalf("%s %s: expected %s, got %+v (ok=%v)", tc.name, k, tc.want, a, ok)
			}
		}
	}
}

func TestQUICFloor(t *testing.T) {
	bw := 10
	rtt := 50.0 // bdp = 10 * 125000 * 0.05 * 2 = 125000 -> clamped to 4194304, but QUIC floor raises rmem_max/wmem_max to 8388608
	in := Inputs{
		MemoryBytes:     330 * 1024 * 1024,
		Keys:            allWritableKeys(),
		HasQUICInbound:  true,
		BandwidthMbps:   &bw,
		MedianRTTMillis: &rtt,
		Current: map[string]string{
			"net.core.rmem_max": "212992",
			"net.core.wmem_max": "212992",
		},
	}
	got := Recommend(in)
	for _, k := range []string{"net.core.rmem_max", "net.core.wmem_max"} {
		a, ok := findAdvice(got, k)
		if !ok || a.Value != "8388608" {
			t.Fatalf("%s: expected 8388608 with QUIC floor, got %+v (ok=%v)", k, a, ok)
		}
		if !strings.Contains(a.Reason, "QUIC") {
			t.Fatalf("%s: expected merged QUIC reason, got %q", k, a.Reason)
		}
	}
}

func TestBDPCap(t *testing.T) {
	bw := 100
	rtt := 200.0 // 100 * 125000 * 0.2 * 2 = 5000000
	in := Inputs{
		MemoryBytes:     2 * 1024 * 1024 * 1024,
		Keys:            allWritableKeys(),
		BandwidthMbps:   &bw,
		MedianRTTMillis: &rtt,
		Current: map[string]string{
			"net.core.rmem_max": "212992",
			"net.core.wmem_max": "212992",
			"net.ipv4.tcp_rmem": "4096 131072 4194304",
			"net.ipv4.tcp_wmem": "4096 16384 4194304",
		},
	}
	got := Recommend(in)
	for _, k := range []string{"net.core.rmem_max", "net.core.wmem_max", "net.ipv4.tcp_rmem", "net.ipv4.tcp_wmem"} {
		a, ok := findAdvice(got, k)
		if !ok || a.Value != "5000000" {
			t.Fatalf("%s: expected 5000000, got %+v (ok=%v)", k, a, ok)
		}
	}
}

func TestRelayMTUProbing(t *testing.T) {
	in := Inputs{
		MemoryBytes: 1024 * 1024 * 1024,
		Keys:        allWritableKeys(),
		RelayRole:   true,
		Current: map[string]string{
			"net.ipv4.tcp_mtu_probing": "0",
		},
	}
	got := Recommend(in)
	a, ok := findAdvice(got, "net.ipv4.tcp_mtu_probing")
	if !ok || a.Value != "1" || a.Level != "optional" || a.Rule != "mtu-probing" {
		t.Fatalf("expected optional mtu-probing=1, got %+v (ok=%v)", a, ok)
	}
}

func TestThreeProductionHosts(t *testing.T) {
	// 1. us2: KVM, 2 cores, 2 GiB, bbr/fq already set via 99-bbr-qdisc.conf
	us2 := Recommend(Inputs{
		MemoryBytes:         2 * 1024 * 1024 * 1024,
		CPUCores:            2,
		Virtualization:      "kvm",
		Keys:                allWritableKeys(),
		CongestionAvailable: []string{"reno", "cubic", "bbr"},
		QdiscAvailable:      []string{"fq", "fq_codel", "pfifo_fast"},
		Sources: map[string]string{
			"net.ipv4.tcp_congestion_control": "99-bbr-qdisc.conf",
			"net.core.default_qdisc":          "99-bbr-qdisc.conf",
		},
		Current: map[string]string{
			"net.ipv4.tcp_congestion_control":    "bbr",
			"net.core.default_qdisc":             "fq",
			"net.ipv4.tcp_slow_start_after_idle": "1",
			"net.ipv4.tcp_notsent_lowat":         "4294967295",
			"net.core.rmem_max":                  "212992",
			"net.core.wmem_max":                  "212992",
			"net.ipv4.tcp_rmem":                  "4096 131072 6291456",
			"net.ipv4.tcp_wmem":                  "4096 16384 4194304",
			"net.ipv4.tcp_mtu_probing":           "0",
		},
	})
	if _, ok := findAdvice(us2, "net.ipv4.tcp_congestion_control"); ok {
		t.Fatal("us2 should not advise bbr when already bbr")
	}
	if _, ok := findAdvice(us2, "net.core.default_qdisc"); ok {
		t.Fatal("us2 should not advise fq when already fq")
	}
	if a, ok := findAdvice(us2, "net.ipv4.tcp_slow_start_after_idle"); !ok || a.Value != "0" {
		t.Fatalf("us2 expected tcp_slow_start_after_idle=0, got %+v", a)
	}
	if a, ok := findAdvice(us2, "net.ipv4.tcp_notsent_lowat"); !ok || a.Value != "131072" {
		t.Fatalf("us2 expected tcp_notsent_lowat=131072, got %+v", a)
	}
	if a, ok := findAdvice(us2, "net.core.rmem_max"); !ok || a.Value != "33554432" {
		t.Fatalf("us2 expected rmem_max=33554432, got %+v", a)
	}

	// 2. uk: KVM, 1 core, ~930 MB (or 1 GiB class), bbr/fq already set
	uk := Recommend(Inputs{
		MemoryBytes:         930 * 1024 * 1024,
		CPUCores:            1,
		Virtualization:      "kvm",
		Keys:                allWritableKeys(),
		CongestionAvailable: []string{"reno", "cubic", "bbr"},
		QdiscAvailable:      []string{"fq", "fq_codel", "pfifo_fast"},
		Current: map[string]string{
			"net.ipv4.tcp_congestion_control":    "bbr",
			"net.core.default_qdisc":             "fq",
			"net.ipv4.tcp_slow_start_after_idle": "1",
			"net.ipv4.tcp_notsent_lowat":         "4294967295",
			"net.core.rmem_max":                  "212992",
			"net.core.wmem_max":                  "212992",
			"net.ipv4.tcp_rmem":                  "4096 131072 6291456",
			"net.ipv4.tcp_wmem":                  "4096 16384 4194304",
		},
	})
	if a, ok := findAdvice(uk, "net.core.rmem_max"); !ok || a.Value != "8388608" {
		t.Fatalf("uk (930MB < 1GiB) expected rmem_max=8388608, got %+v", a)
	}

	// 3. alpine: LXC, 1 core, 330 MB, cubic, no net.core.*
	alpine := Recommend(Inputs{
		MemoryBytes:         330 * 1024 * 1024,
		CPUCores:            1,
		Virtualization:      "lxc",
		Keys:                lxcKeys(),
		CongestionAvailable: []string{"cubic", "reno"},
		Current: map[string]string{
			"net.ipv4.tcp_congestion_control":    "cubic",
			"net.ipv4.tcp_slow_start_after_idle": "1",
			"net.ipv4.tcp_notsent_lowat":         "4294967295",
			"net.ipv4.tcp_rmem":                  "4096 131072 6291456",
			"net.ipv4.tcp_wmem":                  "4096 16384 4194304",
		},
	})
	for _, a := range alpine {
		if strings.HasPrefix(a.Key, "net.core.") {
			t.Fatalf("alpine should not contain net.core.*: %+v", a)
		}
	}
	if _, ok := findAdvice(alpine, "net.ipv4.tcp_congestion_control"); ok {
		t.Fatal("alpine without bbr module should not advise bbr")
	}
	if a, ok := findAdvice(alpine, "net.ipv4.tcp_rmem"); !ok || a.Value != "8388608" {
		t.Fatalf("alpine expected tcp_rmem=8388608, got %+v", a)
	}
}
