package node

import (
	"errors"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// maxSettingsCombinations bounds one report so a malformed Agent cannot inflate
// the stored capability document.
const maxSettingsCombinations = 64

func settingsCombination(combination *pb.SettingsCombination) desiredstate.SettingsCombination {
	shared := desiredstate.SettingsCombination{
		Protocol: combination.Protocol, Direction: combination.Direction, Security: combination.Security,
		Transport: combination.Transport, Flow: combination.Flow,
	}
	// The same normalization ServerSettingsCombination applies when it derives a
	// requirement, so one combination has one spelling on both sides.
	if shared.Security == "" {
		shared.Security = "none"
	}
	if shared.Transport == "" {
		shared.Transport = "tcp"
	}
	return shared
}

// SettingsContractCapability converts one enabled core's settings-contract
// report into the shared desiredstate contract (compatibility plan §13.3 item 3).
// It copies what the Agent reported and infers nothing: a missing, zero or
// partially filled report stays exactly as closed as the report itself.
func SettingsContractCapability(core *pb.CoreCapability) desiredstate.CoreSettingsCapability {
	if core == nil {
		return desiredstate.CoreSettingsCapability{}
	}
	combinations := make([]desiredstate.SettingsCombination, 0, len(core.ServerSettingsCombinations))
	for _, combination := range core.ServerSettingsCombinations {
		if combination == nil {
			continue
		}
		combinations = append(combinations, settingsCombination(combination))
	}
	return desiredstate.CoreSettingsCapability{Core: core.Name, Version: core.ServerSettingsContractVersion, Combinations: combinations, CertificateFiles: core.TlsCertificateFilesSupported}
}

// validateSettingsContract checks one report with the shared combination
// validator for understood reports. Unknown versions receive only resource and
// shape checks: accepting them replaces stale grants without interpreting a
// future version's enums. They authorize nothing in RequireSettingsCapability.
func validateSettingsContract(version uint32, combinations []*pb.SettingsCombination) error {
	if len(combinations) > maxSettingsCombinations {
		return errors.New("invalid settings contract")
	}
	if version == 0 && len(combinations) > 0 {
		return errors.New("settings combinations without a contract version")
	}
	seen := map[desiredstate.SettingsCombination]bool{}
	for _, combination := range combinations {
		if combination == nil || len(combination.Protocol) > 64 || len(combination.Direction) > 64 || len(combination.Security) > 64 || len(combination.Transport) > 64 || len(combination.Flow) > 64 {
			return errors.New("invalid settings combination")
		}
		if version != desiredstate.SettingsContractVersion {
			continue
		}
		shared := settingsCombination(combination)
		if !shared.Valid() || seen[shared] {
			return errors.New("invalid settings combination")
		}
		seen[shared] = true
	}
	return nil
}

func KeywordCapability(core *pb.CoreCapability) desiredstate.CoreKeywordCapability {
	if core == nil {
		return desiredstate.CoreKeywordCapability{}
	}
	return desiredstate.CoreKeywordCapability{
		Name:                    core.Name,
		DomainKeywordSupported:  core.DomainKeywordSupported,
		ExpandedPolicySupported: core.ExpandedPolicySupported,
		NativeRuleSetsSupported: core.NativeRuleSetsSupported,
	}
}

func KeywordCapabilities(cores []*pb.CoreCapability) []desiredstate.CoreKeywordCapability {
	out := make([]desiredstate.CoreKeywordCapability, 0, len(cores))
	for _, c := range cores {
		if c != nil {
			out = append(out, KeywordCapability(c))
		}
	}
	return out
}
