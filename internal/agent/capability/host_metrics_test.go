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
