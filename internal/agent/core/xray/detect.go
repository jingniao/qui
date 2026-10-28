package xray

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	pb "proxycontrol/pkg/contracts/agentpb"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// versionPattern matches the official `xray version` banner. V1 runs official
// upstream cores only (plan D1), so the version is the upstream version. A fork
// binary that still prints a fork version suffix is accepted as its base
// upstream version: the suffix is stripped, because no capability may be derived
// from it.
var versionPattern = regexp.MustCompile(`(?m)^Xray\s+([0-9]+\.[0-9]+\.[0-9]+)(?:-proxycontrol\.[0-9]+)?\b`)

// onlineStatsMinimum is the first upstream Xray release with per-user online
// statistics: policy.levels.<level>.statsUserOnline, the GetStatsOnline and
// GetStatsOnlineIpList RPCs and the statsonline / statsonlineiplist commands
// (upstream PR #3637 "API: Add user online stats", released in v24.11.11).
var onlineStatsMinimum = [3]int{24, 11, 11}

type limitedOutput struct{ bytes.Buffer }

func (w *limitedOutput) Write(data []byte) (int, error) {
	if w.Len()+len(data) > 16384 {
		return 0, io.ErrShortBuffer
	}
	return w.Buffer.Write(data)
}

// Detect reports the capabilities of an official upstream Xray binary: the core
// name, the upstream version and the protocol matrix. Optional API facts are
// derived from the version by SupportsStatsAPI and SupportsOnlineStats.
func Detect(ctx context.Context, path string) (*pb.Capabilities, error) {
	executable, err := exec.LookPath(path)
	if err != nil {
		return &pb.Capabilities{}, err
	}
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(timeout, executable, "version")
	output := &limitedOutput{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return &pb.Capabilities{}, err
	}
	match := versionPattern.FindStringSubmatch(output.String())
	if match == nil {
		return &pb.Capabilities{}, errors.New("configured core does not identify as Xray")
	}
	protocols := []*pb.ProtocolCapability{}
	for _, protocol := range []string{"http", "socks5", "shadowsocks", "vless", "trojan"} {
		protocols = append(protocols, &pb.ProtocolCapability{Protocol: protocol, Inbound: true, Outbound: true, Tcp: true, Udp: protocol != "http"})
	}
	major, _ := strconv.Atoi(strings.Split(match[1], ".")[0])
	protocols = append(protocols, &pb.ProtocolCapability{Protocol: "wireguard", Inbound: major >= 25, Outbound: true, Tcp: true, Udp: true})
	caps := &pb.Capabilities{CoreName: "xray", CoreVersion: match[1], Protocols: protocols, ForwardInboundSupported: true}
	caps.DeviceGateSupported = SupportsOnlineStats(caps)
	if caps.DeviceGateSupported {
		caps.ObservationContractVersion = 2
	}
	return caps, nil
}

// SupportsStatsAPI reports whether the detected core exposes the upstream
// StatsService API the agent reads per-user traffic from. Every supported
// upstream Xray release has it, so a detected Xray is always capable.
func SupportsStatsAPI(capabilities *pb.Capabilities) bool {
	return capabilities != nil && capabilities.CoreName == "xray"
}

// SupportsOnlineStats reports whether the detected core exposes per-user online
// statistics. Older releases report traffic only, so the agent degrades to
// "unknown" online counts instead of guessing.
func SupportsOnlineStats(capabilities *pb.Capabilities) bool {
	return SupportsStatsAPI(capabilities) && versionAtLeast(capabilities.CoreVersion, onlineStatsMinimum)
}

// versionAtLeast reports whether a `major.minor.patch` upstream version is at
// least the given floor. An unparsable version is never assumed capable.
func versionAtLeast(value string, minimum [3]int) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		if number != minimum[index] {
			return number > minimum[index]
		}
	}
	return true
}
