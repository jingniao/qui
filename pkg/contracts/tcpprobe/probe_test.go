package tcpprobe

import (
	"context"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net"
	"net/netip"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestTCPProbeAddressPolicyAndLimits(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "100.64.1.1", "169.254.1.1", "fe80::1", "fc00::1", "224.0.0.1", "ff02::1", "0.0.0.0", "::", "::ffff:127.0.0.1",
		"0.1.2.3", "192.0.0.8", "198.18.0.1", "240.0.0.1", "64:ff9b::a00:1", "64:ff9b:1::1", "100::1", "2001:db8::1", "2002:a00:1::1"} {
		if Public(netip.MustParseAddr(s)) {
			t.Errorf("accepted %s", s)
		}
	}
	c := &pb.TCPProbeCommand{Id: "test", Addresses: []string{"1.1.1.1"}, Port: 443, Attempts: 10, TimeoutMs: 3000, Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	if err := Validate(c, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*pb.TCPProbeCommand){func(c *pb.TCPProbeCommand) { c.Attempts = 11 }, func(c *pb.TCPProbeCommand) { c.TimeoutMs = 3001 }, func(c *pb.TCPProbeCommand) { c.Addresses = []string{"example.com"} }, func(c *pb.TCPProbeCommand) { c.Addresses = []string{"127.0.0.1"} }, func(c *pb.TCPProbeCommand) { c.Deadline = timestamppb.New(time.Now().Add(-time.Second)) }} {
		bad := proto.Clone(c).(*pb.TCPProbeCommand)
		mutate(bad)
		if Validate(bad, time.Now()) == nil {
			t.Fatal("accepted invalid command")
		}
	}
}

func TestTCPProbeConnectMedianLossAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	c := &pb.TCPProbeCommand{Id: "test", Addresses: []string{"127.0.0.1"}, Port: uint32(listener.Addr().(*net.TCPAddr).Port), Attempts: 4, TimeoutMs: 100, AllowNonPublic: true, Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	r := Run(context.Background(), c)
	if r.Attempted != 4 || r.Lost != 0 || r.MedianMs == nil || *r.MedianMs < 0 {
		t.Fatalf("%+v", r)
	}
	listener.Close()
	r = Run(context.Background(), c)
	if r.Lost != 4 || r.MedianMs != nil {
		t.Fatalf("%+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	r = Run(ctx, c)
	if r.Code != "cancelled" || time.Since(start) > time.Second {
		t.Fatalf("cancellation: %+v", r)
	}
	c.AllowNonPublic = false
	if r = Run(context.Background(), c); r.Attempted != 0 {
		t.Fatal("dialed refused target")
	}
}
