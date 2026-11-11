package capability

import (
	"os"
	"path/filepath"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestHostMetricsProcReadOnlyAndMissing(t *testing.T) {
	root := t.TempDir()
	h := &pb.Heartbeat{}
	readHost(root, h)
	if h.HostUptimeSeconds != nil || h.TcpCongestionControl != nil || h.DefaultQdisc != nil || len(h.Interfaces) != 0 {
		t.Fatal("missing proc fields fabricated", h)
	}
	fixtures := map[string]string{"uptime": "123.25 80.0\n", "sys/net/ipv4/tcp_congestion_control": "bbr\n", "sys/net/core/default_qdisc": "fq\n", "net/dev": "lo: 9 0 0 0 0 0 0 0 9 0 0 0 0 0 0 0\neth0: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\nbad: x\n"}
	for path, content := range fixtures {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0400); err != nil {
			t.Fatal(err)
		}
	}
	readHost(root, h)
	if h.HostUptimeSeconds == nil || *h.HostUptimeSeconds != 123.25 || h.GetTcpCongestionControl() != "bbr" || h.GetDefaultQdisc() != "fq" || len(h.Interfaces) != 1 || h.Interfaces[0].ReceiveBytes != 100 || h.Interfaces[0].TransmitBytes != 200 || h.Interfaces[0].ReceiveBytesPerSecond != nil {
		t.Fatal(h)
	}
	for path, content := range fixtures {
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(raw) != content {
			t.Fatal("proc read changed fixture", path, err)
		}
	}
}

func TestNICRatesUseElapsedClockAndCounterResetIsUnknown(t *testing.T) {
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	s := &Sampler{sampled: now, interfaces: map[string][2]uint64{"eth0": {100, 200}}}
	h := &pb.Heartbeat{Interfaces: []*pb.InterfaceCounters{{Name: "eth0", ReceiveBytes: 160, TransmitBytes: 320}, {Name: "eth1", ReceiveBytes: 10}}}
	s.interfaceRates(h, now.Add(2*time.Second))
	if h.Interfaces[0].GetReceiveBytesPerSecond() != 30 || h.Interfaces[0].GetTransmitBytesPerSecond() != 60 || h.Interfaces[1].ReceiveBytesPerSecond != nil {
		t.Fatal(h)
	}
	h = &pb.Heartbeat{Interfaces: []*pb.InterfaceCounters{{Name: "eth0", ReceiveBytes: 1, TransmitBytes: 2}}}
	s.interfaceRates(h, now.Add(3*time.Second))
	if h.Interfaces[0].ReceiveBytesPerSecond != nil || h.Interfaces[0].TransmitBytesPerSecond != nil {
		t.Fatal("reset counters fabricated a rate", h)
	}
}

func TestShortIntervalHeartbeatsReuseCPUAndNICRatesWithoutResettingBaseline(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	s := &Sampler{sampled: t0, previous: counters{total: 1000, idle: 900, valid: true}, interfaces: map[string][2]uint64{"eth0": {100, 200}}}
	beat := func(at time.Time, cpu counters, rx, tx uint64) *pb.Heartbeat {
		h := &pb.Heartbeat{Interfaces: []*pb.InterfaceCounters{{Name: "eth0", ReceiveBytes: rx, TransmitBytes: tx}}}
		s.interfaceRates(h, at)
		s.applyCPU(h, cpu, at)
		return h
	}
	first := beat(t0.Add(2*time.Second), counters{total: 1200, idle: 1000, valid: true}, 160, 320)
	if first.GetCpuPercent() != 50 || first.Interfaces[0].GetReceiveBytesPerSecond() != 30 || first.Interfaces[0].GetTransmitBytesPerSecond() != 60 {
		t.Fatal(first)
	}
	// 100 ms later: values are reused and the baseline stays at the first sample.
	second := beat(t0.Add(2100*time.Millisecond), counters{total: 1210, idle: 1010, valid: true}, 170, 330)
	if second.GetCpuPercent() != 50 || second.Interfaces[0].GetReceiveBytesPerSecond() != 30 || second.Interfaces[0].GetTransmitBytesPerSecond() != 60 {
		t.Fatal("short interval lost readings", second)
	}
	if !s.sampled.Equal(t0.Add(2*time.Second)) || s.previous.total != 1200 || s.interfaces["eth0"] != [2]uint64{160, 320} {
		t.Fatal("short interval moved the baseline", s.sampled, s.previous, s.interfaces)
	}
	// A NIC that disappeared is not given a stale rate.
	h := &pb.Heartbeat{Interfaces: []*pb.InterfaceCounters{{Name: "eth9", ReceiveBytes: 1}}}
	s.interfaceRates(h, t0.Add(2200*time.Millisecond))
	if h.Interfaces[0].ReceiveBytesPerSecond != nil {
		t.Fatal(h)
	}
	// The next full interval measures against the first baseline (4 s since t0+2s).
	third := beat(t0.Add(4*time.Second), counters{total: 1600, idle: 1200, valid: true}, 280, 520)
	if third.GetCpuPercent() != 50 || third.Interfaces[0].GetReceiveBytesPerSecond() != 60 {
		t.Fatal(third)
	}
}

func procFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0400); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestHostBootIDIsCanonicalOrUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"canonical", "1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60\n", "1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60"},
		{"no groups", "1c0b5a6e7f6b4f2f9c3a1b2c3d4e5f60\n", ""},
		{"not hex", "zzzzzzzz-7f6b-4f2f-9c3a-1b2c3d4e5f60\n", ""},
		{"too short", "1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f6\n", ""},
		{"empty", "\n", ""},
	} {
		h := &pb.Heartbeat{}
		readHost(procFixture(t, map[string]string{"sys/kernel/random/boot_id": tc.content}), h)
		if h.GetBootId() != tc.want || (h.BootId != nil) != (tc.want != "") {
			t.Fatalf("%s: %q", tc.name, h.GetBootId())
		}
	}
	h := &pb.Heartbeat{}
	readHost(t.TempDir(), h)
	if h.BootId != nil {
		t.Fatal("a missing boot id was fabricated", h.GetBootId())
	}
}

func TestHostDefaultRouteInterfaceHeuristic(t *testing.T) {
	header := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"
	row := func(name, destination, gateway, flags, metric, mask string) string {
		return name + "\t" + destination + "\t" + gateway + "\t" + flags + "\t0\t0\t" + metric + "\t" + mask + "\t0\t0\t0\n"
	}
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"smallest metric", header + row("eth1", "00000000", "0102A8C0", "0003", "300", "00000000") + row("eth0", "00000000", "0102A8C0", "0003", "100", "00000000"), "eth0"},
		{"name breaks the metric tie", header + row("ens18", "00000000", "0102A8C0", "0003", "100", "00000000") + row("eth0", "00000000", "0102A8C0", "0003", "100", "00000000"), "ens18"},
		{"zero gateway is a default route", header + row("eth0", "00000000", "00000000", "0003", "100", "00000000"), "eth0"},
		{"subnet route is not a default", header + row("eth0", "0000A8C0", "0102A8C0", "0003", "100", "00FFFFFF"), ""},
		{"masked default is not a default", header + row("eth0", "00000000", "0102A8C0", "0003", "100", "00FFFFFF"), ""},
		{"missing rtf_up", header + row("eth0", "00000000", "0102A8C0", "0002", "100", "00000000"), ""},
		{"rtf_reject", header + row("eth0", "00000000", "0102A8C0", "0203", "100", "00000000"), ""},
		{"loopback is never selected", header + row("lo", "00000000", "00000000", "0003", "0", "00000000"), ""},
		{"malformed flags", header + row("eth0", "00000000", "0102A8C0", "zzzz", "100", "00000000"), ""},
		{"malformed metric", header + row("eth0", "00000000", "0102A8C0", "0003", "one", "00000000"), ""},
		{"short row", header + "eth0\t00000000\t0003\n", ""},
		{"no ipv4 route", header, ""},
		{"empty file", "", ""},
	} {
		got, ok := defaultRouteInterface(tc.content)
		if got != tc.want || ok != (tc.want != "") {
			t.Fatalf("%s: %q %v", tc.name, got, ok)
		}
	}
}

func TestHostIdentitySurvivesInterfaceReadFailure(t *testing.T) {
	root := procFixture(t, map[string]string{
		"sys/kernel/random/boot_id": "1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60\n",
		"net/route":                 "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\neth0\t00000000\t0102A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
	})
	h := &pb.Heartbeat{}
	readHost(root, h)
	if h.GetBootId() != "1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60" || h.GetDefaultRouteInterface() != "eth0" {
		t.Fatalf("a failed /proc/net/dev read erased the host identity: %v", h)
	}
	if len(h.Interfaces) != 0 {
		t.Fatal("a missing /proc/net/dev fabricated interfaces", h.Interfaces)
	}
	h = &pb.Heartbeat{}
	readHost(procFixture(t, map[string]string{"net/dev": "eth0: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\n"}), h)
	if h.BootId != nil || h.DefaultRouteInterface != nil || len(h.Interfaces) != 1 {
		t.Fatalf("missing identity files fabricated values: %v", h)
	}
}
