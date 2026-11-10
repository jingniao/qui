package capability

import (
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestReadSNMPCounters(t *testing.T) {
	sample := "Ip: Forwarding DefaultTTL InReceives\n" +
		"Ip: 1 64 1000\n" +
		"Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors\n" +
		"Tcp: 1 200 120000 -1 123 45 2 1 5 9000 100000 120 0 3 0\n" +
		"Udp: InDatagrams NoPorts InErrors OutDatagrams\n" +
		"Udp: 50 0 0 50\n"

	h := &pb.Heartbeat{}
	readHost(procFixture(t, map[string]string{"net/snmp": sample}), h)
	if h.TcpOutSegs == nil || *h.TcpOutSegs != 100000 {
		t.Fatalf("expected TcpOutSegs=100000, got %v", h.TcpOutSegs)
	}
	if h.TcpRetransSegs == nil || *h.TcpRetransSegs != 120 {
		t.Fatalf("expected TcpRetransSegs=120, got %v", h.TcpRetransSegs)
	}

	// Missing file or malformed Tcp section leaves pointers nil
	empty := &pb.Heartbeat{}
	readHost(t.TempDir(), empty)
	if empty.TcpOutSegs != nil || empty.TcpRetransSegs != nil {
		t.Fatalf("expected nil counters when net/snmp missing, got %v %v", empty.TcpOutSegs, empty.TcpRetransSegs)
	}

	bad := &pb.Heartbeat{}
	readHost(procFixture(t, map[string]string{"net/snmp": "Tcp: OutSegs RetransSegs\nTcp: 100\n"}), bad)
	if bad.TcpOutSegs != nil || bad.TcpRetransSegs != nil {
		t.Fatalf("expected nil counters on mismatched field count, got %v %v", bad.TcpOutSegs, bad.TcpRetransSegs)
	}
}
