// Package tcpprobe defines the fixed, bounded TCP-connect operation.
package tcpprobe

import (
	"context"
	"errors"
	"math"
	"net"
	"net/netip"
	pb "proxycontrol/pkg/contracts/agentpb"
	"sort"
	"strconv"
	"time"
)

// special holds non-public ranges that IsGlobalUnicast still accepts, the same
// list as the subscription importer. NAT64 and 6to4 can carry an internal IPv4.
var special = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func Public(address netip.Addr) bool {
	a := address.Unmap()
	if !a.IsValid() || a.Zone() != "" || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range special {
		if prefix.Contains(a) {
			return false
		}
	}
	return true
}

func Validate(c *pb.TCPProbeCommand, now time.Time) error {
	if c == nil || c.Id == "" || len(c.Id) > 128 || c.Attempts < 1 || c.Attempts > 10 || c.TimeoutMs < 1 || c.TimeoutMs > 3000 || c.Port < 1 || c.Port > 65535 || len(c.Addresses) < 1 || len(c.Addresses) > 16 || c.Deadline == nil || c.Deadline.CheckValid() != nil || !c.Deadline.AsTime().After(now) || c.Deadline.AsTime().After(now.Add(5*time.Minute)) {
		return errors.New("invalid or expired TCP probe limits")
	}
	for _, raw := range c.Addresses {
		a, err := netip.ParseAddr(raw)
		if err != nil || a.Zone() != "" || !c.AllowNonPublic && !Public(a) {
			return errors.New("TCP probe address refused")
		}
	}
	return nil
}

// ValidateResult binds a result to its original limits without requiring DNS or
// a still-live deadline, so the same check also works for retained backups.
func ValidateResult(c *pb.TCPProbeCommand, r *pb.TCPProbeResult) error {
	if c == nil || r == nil || r.Id != c.Id || r.Attempted > c.Attempts || r.Lost > r.Attempted || r.Code != "complete" && r.Code != "invalid" && r.Code != "cancelled" || r.Code == "complete" && r.Attempted != c.Attempts || r.Code == "invalid" && r.Attempted != 0 || (r.Attempted > r.Lost) != (r.MedianMs != nil) {
		return errors.New("invalid TCP probe result")
	}
	if r.MedianMs != nil && (math.IsNaN(*r.MedianMs) || math.IsInf(*r.MedianMs, 0) || *r.MedianMs < 0) {
		return errors.New("invalid TCP probe median")
	}
	return nil
}

// Run only dials literal addresses in the command, with no resolver or shell.
func Run(ctx context.Context, c *pb.TCPProbeCommand) *pb.TCPProbeResult {
	r := &pb.TCPProbeResult{Code: "invalid"}
	if c != nil {
		r.Id = c.Id
	}
	if Validate(c, time.Now()) != nil {
		return r
	}
	ctx, cancel := context.WithDeadline(ctx, c.Deadline.AsTime())
	defer cancel()
	r.Code = "complete"
	latencies := []float64{}
	for i := uint32(0); i < c.Attempts; i++ {
		start := time.Now()
		dialer := net.Dialer{Timeout: time.Duration(c.TimeoutMs) * time.Millisecond}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.Addresses[int(i)%len(c.Addresses)], strconv.Itoa(int(c.Port))))
		r.Attempted++
		if err != nil {
			r.Lost++
		} else {
			latencies = append(latencies, float64(time.Since(start))/float64(time.Millisecond))
			conn.Close()
		}
		if ctx.Err() != nil {
			r.Code = "cancelled"
			break
		}
	}
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		v := latencies[len(latencies)/2]
		if len(latencies)%2 == 0 {
			v = (v + latencies[len(latencies)/2-1]) / 2
		}
		r.MedianMs = &v
	}
	return r
}
