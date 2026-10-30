package capability

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestHostInfoFixtures(t *testing.T) {
	root := filepath.Join("testdata", "host")
	h := readHostInfo(root, filepath.Join(root, "os-release"))
	if h.CpuModel != "Fixture CPU" || h.CpuCores != 2 || h.Architecture != runtime.GOARCH || h.KernelVersion != "6.12.0-fixture" || h.OsPrettyName != "Fixture Linux 1.0" || h.MemoryTotalBytes != 1073741824 || h.SwapTotalBytes != 536870912 {
		t.Fatal(h)
	}
	heartbeat := &pb.Heartbeat{}
	readExtraHost(root, heartbeat)
	if heartbeat.Load1 == nil || heartbeat.GetLoad1() != 0.25 || heartbeat.GetLoad5() != 1.5 || heartbeat.GetLoad15() != 2.75 || heartbeat.TcpConnections == nil || heartbeat.GetTcpConnections() != 3 || heartbeat.UdpSockets == nil || heartbeat.GetUdpSockets() != 1 || heartbeat.GetMemoryUsedBytes() != 805306368 || heartbeat.GetSwapUsedBytes() != 402653184 {
		t.Fatal(heartbeat)
	}
}
func TestUnreadableHostMetricsAreUnset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	h := &pb.Heartbeat{}
	readExtraHost(root, h)
	if h.Load1 != nil || h.ProcessCount != nil || h.TcpConnections != nil || h.UdpSockets != nil || h.MemoryUsedBytes != nil || h.SwapUsedBytes != nil {
		t.Fatal(h)
	}
}
func TestProcessCountAndMalformedMetrics(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"1", "234", "self", "net"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "loadavg"), []byte("NaN -1 broken"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &pb.Heartbeat{}
	readExtraHost(root, h)
	if h.ProcessCount == nil || h.GetProcessCount() != 2 || h.Load1 != nil || h.Load5 != nil || h.Load15 != nil {
		t.Fatal(h)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte("bad header\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if socketCount(root, "tcp") != nil {
		t.Fatal("malformed sockets fabricated count")
	}
}
