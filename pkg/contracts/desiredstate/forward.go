package desiredstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

// Forward settings bounds (plan G2). They are implementation limits that keep a
// lightweight node's listener and configuration size predictable, not Owner
// choices: one forward keeps at most 32 targets, at most 256 listening ports and
// a node renders at most 1024 production services.
const (
	MaxForwardTargets     = 32
	MaxForwardListenPorts = 256
	MinForwardBandwidth   = int64(1024)
	MaxForwardBandwidth   = int64(1_000_000_000_000)
	MaxForwardConnections = 100000
	MaxForwardFailTimeout = 3600
	MaxForwardFailures    = 100
)

// Forward selector defaults. They are rendered when the Owner leaves the
// optional fields out, so a pool without parameters still behaves the same way.
const (
	DefaultForwardStrategy    = "round"
	DefaultForwardMaxFails    = 1
	DefaultForwardFailTimeout = "5s"
)

// GostForwardContractVersion is the GOST settings contract that first carries
// target pools, port ranges and bandwidth/connection limits (G2). A core that
// reports less cannot render them, so the panel refuses the options instead of
// publishing a configuration that silently ignores them.
const GostForwardContractVersion = 2

// ForwardTarget is one member of a forward's ordered target pool.
type ForwardTarget struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// UnmarshalJSON keeps the "was a pool written" distinction: an explicit
// `targets: []` is invalid, while an omitted list means the legacy single target.
// Unknown fields stay rejected, so the strict decoding of every caller is kept.
func (f *ForwardSettings) UnmarshalJSON(data []byte) error {
	type plain ForwardSettings
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var presence struct {
		Targets json.RawMessage `json:"targets"`
	}
	if err := json.Unmarshal(data, &presence); err != nil {
		return err
	}
	*f = ForwardSettings(decoded)
	f.poolWritten = presence.Targets != nil
	return nil
}

// GostForwardPorts reports whether the settings change the listening surface of a
// forward that only the GOST core can render: an expanded port range.
func (f *ForwardSettings) GostForwardPorts() bool { return f != nil && f.PortEnd != nil }

// HasGostOptions reports whether the forward carries any option the GOST
// settings contract version 2 added. Every layer asks this one function, so a
// core that cannot render them refuses the inbound instead of silently dropping
// the fields. Presence-based: an explicitly written zero or default counts.
func (f *ForwardSettings) HasGostOptions() bool {
	if f == nil {
		return false
	}
	return len(f.Targets) > 0 || f.Strategy != nil || f.MaxFails != nil || f.FailTimeout != nil ||
		f.PortEnd != nil || f.BandwidthInBps != nil || f.BandwidthOutBps != nil || f.MaxConnections != nil
}

// TargetPool is the ordered target list the renderer emits: the explicit pool
// when one was configured, otherwise the single legacy host/port pair.
func (f *ForwardSettings) TargetPool() []ForwardTarget {
	if f == nil {
		return nil
	}
	if len(f.Targets) > 0 {
		return f.Targets
	}
	return []ForwardTarget{{Host: f.Host, Port: f.Port}}
}

// Selector is the normalized forwarder selector: the strategy, the failure count
// that marks a member down and the window before it is tried again.
func (f *ForwardSettings) Selector() (strategy string, maxFails int, failTimeout string) {
	strategy, maxFails, failTimeout = DefaultForwardStrategy, DefaultForwardMaxFails, DefaultForwardFailTimeout
	if f == nil {
		return strategy, maxFails, failTimeout
	}
	if f.Strategy != nil {
		strategy = *f.Strategy
	}
	if f.MaxFails != nil {
		maxFails = *f.MaxFails
	}
	if f.FailTimeout != nil {
		failTimeout = *f.FailTimeout
	}
	return strategy, maxFails, failTimeout
}

// ListenPorts returns the inclusive listening ports of one forward: the base port
// alone, or the base port through the configured end port.
func (f *ForwardSettings) ListenPorts(base int) []int {
	ports := []int{base}
	if f == nil || f.PortEnd == nil || *f.PortEnd <= base {
		return ports
	}
	for port := base + 1; port <= *f.PortEnd && len(ports) < MaxForwardListenPorts; port++ {
		ports = append(ports, port)
	}
	return ports
}

// ListenPorts is the shared numeric-port expansion helper. Every layer that
// reserves, allocates or reports a listener uses it: the Agent's DesiredState
// validation and reservation, the panel's node-wide conflict check, the canvas
// planner and the archive validation. Only a GOST forward can expand today, so a
// non-forward inbound is exactly its own port.
func ListenPorts(port int, settings ProtocolSettings) []int {
	if settings.Forward == nil {
		return []int{port}
	}
	return settings.Forward.ListenPorts(port)
}

// ValidateForwardCore rejects the GOST-only forward options on a core that cannot
// render them. core is the resolved core name: callers resolve the primary before
// asking, so an inbound that leaves its core empty is checked against the core
// that will actually run it.
func ValidateForwardCore(core string, settings ProtocolSettings) error {
	if !settings.Forward.HasGostOptions() || core == CoreGost {
		return nil
	}
	return errors.New("forward targets, ranges and limits require the gost core")
}

// ValidateListenPorts checks the numeric listener range of one inbound: a valid
// base port, an end not below it and at most MaxForwardListenPorts ports. It is
// the range half of Validate, for callers that hold a stored inbound (an archive
// being restored) instead of a settings request.
func ValidateListenPorts(port int, settings ProtocolSettings) error {
	if port < 1 || port > 65535 {
		return errors.New("invalid inbound listener")
	}
	end := settings.Forward
	if !end.GostForwardPorts() {
		return nil
	}
	if *end.PortEnd < port || *end.PortEnd > 65535 {
		return errors.New("forward end port must be between the listening port and 65535")
	}
	if *end.PortEnd-port+1 > MaxForwardListenPorts {
		return errors.New("a forward can listen on at most 256 ports")
	}
	return nil
}

// Validate checks the typed forward settings. It never resolves a host: whether a
// destination answers is decided on the node when the traffic is forwarded.
func (f *ForwardSettings) Validate() error {
	if f == nil || (f.Network != "tcp" && f.Network != "udp" && f.Network != "tcp,udp") {
		return errors.New("forward requires a target host, port and tcp/udp network")
	}
	if f.poolWritten && len(f.Targets) == 0 {
		return errors.New("a forward target pool cannot be empty")
	}
	if len(f.Targets) > 0 {
		// A pool and the legacy single target are two representations of the same
		// thing; accepting both would leave it to guesswork which one wins.
		if f.Host != "" || f.Port != 0 {
			return errors.New("forward targets cannot be combined with a single host and port")
		}
		if len(f.Targets) > MaxForwardTargets {
			return errors.New("a forward supports at most 32 targets")
		}
		seen := map[string]bool{}
		for _, target := range f.Targets {
			if !validForwardHost(target.Host) || target.Port < 1 || target.Port > 65535 {
				return errors.New("forward requires a target host, port and tcp/udp network")
			}
			key := targetKey(target)
			if seen[key] {
				return errors.New("forward targets must be unique")
			}
			seen[key] = true
		}
	} else if !validForwardHost(f.Host) || f.Port < 1 || f.Port > 65535 {
		return errors.New("forward requires a target host, port and tcp/udp network")
	}
	if f.Strategy != nil && *f.Strategy != "round" && *f.Strategy != "fifo" {
		return errors.New("forward strategy must be round or fifo")
	}
	if f.MaxFails != nil && (*f.MaxFails < 1 || *f.MaxFails > MaxForwardFailures) {
		return errors.New("forward max_fails must be between 1 and 100")
	}
	if f.FailTimeout != nil && !validFailTimeout(*f.FailTimeout) {
		return errors.New("forward fail_timeout must be whole seconds between 1s and 3600s")
	}
	if f.PortEnd != nil && (*f.PortEnd < 1 || *f.PortEnd > 65535) {
		return errors.New("forward end port must be between the listening port and 65535")
	}
	for _, rate := range []*int64{f.BandwidthInBps, f.BandwidthOutBps} {
		if rate != nil && *rate != 0 && (*rate < MinForwardBandwidth || *rate > MaxForwardBandwidth) {
			return errors.New("forward bandwidth must be 0 or 1024-1000000000000 bytes per second")
		}
	}
	if f.MaxConnections != nil {
		if *f.MaxConnections < 0 || *f.MaxConnections > MaxForwardConnections {
			return errors.New("forward max_connections must be between 0 and 100000")
		}
		// G0 measured TCP concurrency only; a UDP-only forward has nothing to
		// count, so the field is refused rather than rendered and ignored.
		if f.Network == "udp" {
			return errors.New("forward max_connections requires a TCP service")
		}
	}
	return nil
}

// targetKey normalizes one pool member for the duplicate check: an IP literal in
// its canonical text, a host name in lower case.
func targetKey(target ForwardTarget) string {
	if address, err := netip.ParseAddr(target.Host); err == nil {
		return address.String() + "|" + strconv.Itoa(target.Port)
	}
	return strings.ToLower(target.Host) + "|" + strconv.Itoa(target.Port)
}

// validFailTimeout accepts whole seconds in the canonical `<n>s` form and nothing
// else: a bare number, a fraction or a larger unit would be normalized by nobody
// and silently changed later.
func validFailTimeout(value string) bool {
	if !strings.HasSuffix(value, "s") {
		return false
	}
	digits := strings.TrimSuffix(value, "s")
	if digits == "" || len(digits) > 1 && digits[0] == '0' {
		return false
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	seconds, err := strconv.Atoi(digits)
	return err == nil && seconds >= 1 && seconds <= MaxForwardFailTimeout
}
