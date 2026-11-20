package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"proxycontrol/internal/agent/capability"
	"proxycontrol/internal/agent/hostnet"
	"proxycontrol/internal/agent/tuning"
	pb "proxycontrol/pkg/contracts/agentpb"
	tuningcontract "proxycontrol/pkg/contracts/tuning"
)

func reportCapabilities(s *capability.Sampler, c *capabilityState, hostDirectory string) *pb.Capabilities {
	result := c.Snapshot()
	if len(result.Cores) == 0 && result.CoreName != "" {
		result.Cores = []*pb.CoreCapability{{Name: result.CoreName, Version: result.CoreVersion, Protocols: result.Protocols, SingboxUserStats: result.SingboxUserStats, ForwardInboundSupported: result.ForwardInboundSupported, DeviceGateSupported: result.DeviceGateSupported, ObservationContractVersion: result.ObservationContractVersion, ServerSettingsContractVersion: result.ServerSettingsContractVersion, ServerSettingsCombinations: result.ServerSettingsCombinations, TlsCertificateFilesSupported: result.TlsCertificateFilesSupported, DomainKeywordSupported: result.DomainKeywordSupported, ExpandedPolicySupported: result.ExpandedPolicySupported, NativeRuleSetsSupported: result.NativeRuleSetsSupported, OutboundSourceSupported: result.OutboundSourceSupported}}
	}
	result.Host = s.HostInfo()
	result.TuningSupported = tuning.Supported()
	result.PanelCatalogueSupported = true
	result.PortHopSupported = hostnet.HelperInstalled()
	result.HostSourceExtraExemptSupported = hostSourceExtraExemptSupported(hostDirectory, result.PortHopSupported)
	return result
}

func hostSourceExtraExemptSupported(directory string, installed bool) bool {
	if !installed {
		return false
	}
	status, err := hostnet.ReadStatus(directory)
	return err == nil && slices.Contains(status.Features, hostnet.FeatureExtraExempt)
}

// One in-flight request across sessions. Neither a reconnect nor a second
// command may overwrite the desired file while a previous helper is running.
var tuningSlot = make(chan struct{}, 1)

func applyTuning(ctx context.Context, c *pb.TuningCommand) *pb.TuningResult {
	return runTuning(ctx, c, tuning.Directory, "/proc/sys", "/", tuning.Supported())
}
func runTuning(ctx context.Context, c *pb.TuningCommand, directory, proc, hostRoot string, supported bool) *pb.TuningResult {
	result := &pb.TuningResult{RequestId: c.RequestId, Status: "failed"}
	fail := func(err error) *pb.TuningResult {
		result.Keys = []*pb.TuningKeyResult{{Error: err.Error()}}
		return result
	}
	if !supported {
		return fail(fmt.Errorf("tuning helper is not installed"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	desired := tuning.Desired{RequestID: c.RequestId, Settings: c.Settings, Clear: c.Clear}
	if len(c.Settings) > 0 {
		keys, err := tuning.ReadProbe(directory, proc, hostRoot)
		if err != nil {
			return fail(err)
		}
		if err := tuningcontract.CheckProbe(c.Settings, "ready", keys); err != nil {
			return fail(err)
		}
	}
	select {
	case tuningSlot <- struct{}{}:
		defer func() { <-tuningSlot }()
	default:
		return fail(fmt.Errorf("tuning request in progress"))
	}
	if c.InstallMissingModules || tuningcontract.HasModuleTarget(c.Settings) {
		wait, cancel := context.WithTimeout(ctx, 15*time.Minute)
		packages, err := tuning.PrepareModules(wait, desired, directory, proc, hostRoot, c.InstallMissingModules)
		cancel()
		result.InstalledModulePackages = packages
		if err != nil {
			return fail(err)
		}
	}
	if c.InstallMissingModules {
		keys, err := tuning.ReadProbe(directory, proc, hostRoot)
		if err != nil {
			return fail(err)
		}
		if err = tuningcontract.CheckProbe(c.Settings, "ready", keys); err != nil {
			return fail(err)
		}
	}
	if err := desired.Validate(proc); err != nil {
		return fail(err)
	}
	if err := tuning.WriteDesired(directory, desired); err != nil {
		return fail(err)
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, readErr := tuning.ReadStatus(directory)
		if readErr == nil && status.RequestId == c.RequestId {
			status.InstalledModulePackages = result.InstalledModulePackages
			return status
		}
		select {
		case <-wait.Done():
			if errors.Is(readErr, os.ErrNotExist) {
				readErr = nil
			}
			return fail(fmt.Errorf("tuning status unconfirmed: %w", errors.Join(wait.Err(), readErr)))
		case <-ticker.C:
		}
	}
}
