package gost

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

// versionPattern reads the first line of the version banner: `gost 3.3.2
// (go1.27.3 linux/amd64)`. `gost version` is not a subcommand — GOST takes the
// word as a configuration path and fails — so detection always uses `-V`.
var versionPattern = regexp.MustCompile(`(?m)^gost ([0-9]+\.[0-9]+\.[0-9]+) \(`)

// Supported reports whether this build runs the pinned GOST release at all. The
// pin is one statically linked linux/amd64 archive: it serves glibc and musl
// alike, and no other platform or architecture is accepted, whatever upstream
// publishes.
func Supported() bool { return runtime.GOOS == "linux" && runtime.GOARCH == "amd64" }

// Detect reports the capabilities of the pinned official GOST release.
func Detect(ctx context.Context, path string) (*pb.Capabilities, error) {
	executable, err := exec.LookPath(path)
	if err != nil {
		return &pb.Capabilities{}, err
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(deadline, executable, "-V")
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return &pb.Capabilities{}, err
	}
	match := versionPattern.FindStringSubmatch(string(output.Bytes()))
	if match == nil {
		return &pb.Capabilities{}, errors.New("configured core does not identify as gost")
	}
	return capabilitiesFor(match[1]), nil
}

// capabilitiesFor is the detected capability set of one version. The only
// advertised inbound protocol is the forward: relay services are a canvas-managed
// variant of a forward in G3, never a user protocol, and GOST has no account, no
// device gate and no source observation.
func capabilitiesFor(version string) *pb.Capabilities {
	return &pb.Capabilities{
		CoreName:                CoreName,
		CoreVersion:             version,
		ForwardInboundSupported: true,
		Protocols:               []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true, Outbound: false, Tcp: true, Udp: true}},
	}
}

func (a *Adapter) DetectCapabilities(ctx context.Context) (*pb.Capabilities, error) {
	return Detect(ctx, a.ExecutablePath())
}

// DetectVersion runs the executable's own version command. It is the gate that
// runs before a downloaded core is switched in.
func (a *Adapter) DetectVersion(ctx context.Context, path string) (string, error) {
	capabilities, err := Detect(ctx, path)
	if err != nil {
		return "", err
	}
	return capabilities.CoreVersion, nil
}
