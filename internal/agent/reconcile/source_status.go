package reconcile

import (
	"fmt"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Kinds of a configured source binding, as reported in the heartbeat.
const (
	SourceKindCore     = "core_source"     // node core outbound source IP / interface
	SourceKindHost     = "host_source"     // server-wide enforced outbound IPv4 / IPv6
	SourceKindInbound  = "inbound_egress"  // per-inbound egress IP
	SourceKindOutbound = "outbound_source" // per-outbound source IP / interface
)

// SourceIssue is one source binding of the running revision that the host no
// longer has. Address or Interface is empty when the binding does not set it.
type SourceIssue struct {
	Kind, OwnerID, Address, Interface string
}

type sourceTarget struct {
	kind, owner string
	network     desiredstate.Network
}

// MissingSources checks the running revision's source bindings against the
// host's interfaces. Apply refuses a revision whose bindings are absent; this
// check reports bindings that disappeared afterwards (a changed lease, a removed
// address). The retained revision is decrypted only when the running revision's
// digest changes. Read-only: it never alters the runtime.
func (r *Reconciler) MissingSources() (int64, []SourceIssue, error) {
	r.mu.Lock()
	revision, digest := r.manifest.Revision, r.manifest.Digest
	if digest != r.sourceDigest {
		targets, err := r.sourceTargetsLocked(revision)
		if err != nil {
			r.mu.Unlock()
			return revision, nil, err
		}
		r.sourceDigest, r.sourceTargets = digest, targets
	}
	targets := r.sourceTargets
	r.mu.Unlock()
	if len(targets) == 0 {
		return revision, nil, nil
	}
	names, addresses, err := hostInterfaces()
	if err != nil {
		return revision, nil, err
	}
	var issues []SourceIssue
	for _, target := range targets {
		if checkNetworkAvailable(target.network, names, addresses) != nil {
			issues = append(issues, SourceIssue{Kind: target.kind, OwnerID: target.owner, Address: target.network.OutboundSourceIP, Interface: target.network.OutboundInterface})
		}
	}
	return revision, issues, nil
}

func (r *Reconciler) sourceTargetsLocked(revision int64) ([]sourceTarget, error) {
	if revision == 0 {
		return nil, nil
	}
	data, err := r.readRevision(revision)
	if err != nil {
		return nil, fmt.Errorf("read revision %d: %w", revision, err)
	}
	state, err := desiredstate.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode revision %d: %w", revision, err)
	}
	multi, isMulti := r.Adapter.(*core.MultiRuntime)
	return sourceTargets(state, !isMulti || multi.Name() != "", isMulti), nil
}

// sourceTargets lists the bindings apply verifies (verifyOutboundNetworks) plus
// the enforced host sources the helper requires. A node without a rendered core
// keeps only the host sources; a multi-core runtime checks reachable outbounds.
func sourceTargets(state desiredstate.State, coreRendered, reachableOnly bool) []sourceTarget {
	var targets []sourceTarget
	if network := state.Network; network != nil {
		for _, address := range []string{network.HostSourceIPv4, network.HostSourceIPv6} {
			if address != "" {
				targets = append(targets, sourceTarget{kind: SourceKindHost, network: desiredstate.Network{OutboundSourceIP: address}})
			}
		}
		if coreRendered && (network.OutboundSourceIP != "" || network.OutboundInterface != "") {
			targets = append(targets, sourceTarget{kind: SourceKindCore, network: desiredstate.Network{OutboundSourceIP: network.OutboundSourceIP, OutboundInterface: network.OutboundInterface}})
		}
	}
	if !coreRendered {
		return targets
	}
	for _, inbound := range state.Inbounds {
		if inbound.Enabled && inbound.EgressSourceIP != "" {
			targets = append(targets, sourceTarget{kind: SourceKindInbound, owner: inbound.ID, network: desiredstate.Network{OutboundSourceIP: inbound.EgressSourceIP}})
		}
	}
	outbounds := state.Outbounds
	if reachableOnly {
		outbounds = state.ReachableOutbounds()
	}
	for _, outbound := range outbounds {
		if !outbound.Enabled || outbound.Protocol == "block" || outbound.SourceIP == "" && outbound.SourceInterface == "" {
			continue
		}
		network := state.OutboundNetwork(outbound.ID)
		targets = append(targets, sourceTarget{kind: SourceKindOutbound, owner: outbound.ID, network: desiredstate.Network{OutboundSourceIP: network.OutboundSourceIP, OutboundInterface: network.OutboundInterface}})
	}
	return targets
}
