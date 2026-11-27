package core

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
)

// ProbeKey isolates internal observations without changing the original outbound
// identity used in access records and panel health reports. A valid business ID
// cannot contain '|', so a source key cannot collide with a default-source ID.
func ProbeKey(sourceIP, outboundID string) string {
	if sourceIP == "" {
		return outboundID
	}
	return sourceIP + "|" + outboundID
}

// GroupProbes includes every configured member even if the scheduler currently
// chooses Block. Those raw paths must remain measurable so a group can recover.
func GroupProbes(state desiredstate.State) ([]Probe, error) {
	byID := make(map[string]Probe)
	for _, group := range state.OutboundGroups {
		for _, member := range group.Members {
			previous, exists := byID[member.OutboundID]
			if exists && previous.URL != group.ProbeURL {
				return nil, errors.New("outbound has conflicting probe URLs")
			}
			interval := time.Duration(group.ProbeInterval) * time.Second
			if !exists || interval < previous.Interval {
				byID[member.OutboundID] = Probe{OutboundID: member.OutboundID, URL: group.ProbeURL, Interval: interval}
			}
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var probes []Probe
	for _, sourceIP := range append([]string{""}, InboundEgressSources(state)...) {
		for _, id := range ids {
			probe := byID[id]
			probe.SourceIP = sourceIP
			probes = append(probes, probe)
		}
	}
	return probes, nil
}

func ProbeFingerprint(state desiredstate.State, probe Probe) (string, error) {
	var entry desiredstate.Outbound
	found := false
	for _, outbound := range state.Outbounds {
		if outbound.ID == probe.OutboundID {
			entry, found = outbound, true
			break
		}
	}
	if !found {
		return "", errors.New("probe outbound missing")
	}
	network := state.OutboundNetwork(entry.ID)
	if probe.SourceIP != "" && !entry.UseHostNetwork {
		network = desiredstate.Network{OutboundSourceIP: probe.SourceIP}
	}
	credentialIDs := map[string]bool{entry.CredentialID: true}
	if entry.Settings.WireGuard != nil {
		for _, peer := range entry.Settings.WireGuard.Peers {
			credentialIDs[peer.CredentialID] = true
		}
	}
	var credentials []desiredstate.Credential
	for _, credential := range state.Credentials {
		if credentialIDs[credential.ID] {
			credentials = append(credentials, credential)
		}
	}
	slices.SortFunc(credentials, func(a, b desiredstate.Credential) int {
		return cmp.Compare(a.ID, b.ID)
	})
	data, err := json.Marshal(struct {
		Entry         desiredstate.Outbound
		Network       desiredstate.Network
		Credentials   []desiredstate.Credential
		URL, SourceIP string
	}{entry, network, credentials, probe.URL, probe.SourceIP})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (p Probe) Key() string { return ProbeKey(p.SourceIP, p.OutboundID) }

func InboundEgressSources(state desiredstate.State) []string {
	var sources []string
	for _, inbound := range state.Inbounds {
		if inbound.Enabled && inbound.EgressSourceIP != "" && !slices.Contains(sources, inbound.EgressSourceIP) {
			sources = append(sources, inbound.EgressSourceIP)
		}
	}
	slices.Sort(sources)
	return sources
}

// SourceHealth gives the existing scheduler original IDs from one source only.
// An unobserved path stays unknown; the group's existing failure policy applies.
func SourceHealth(state desiredstate.State, health map[string]scheduler.Health, sourceIP string) map[string]scheduler.Health {
	view := make(map[string]scheduler.Health)
	for _, outbound := range state.Outbounds {
		if observed, ok := health[ProbeKey(sourceIP, outbound.ID)]; ok {
			view[outbound.ID] = observed
		}
	}
	return view
}
