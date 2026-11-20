package availability

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"proxycontrol/pkg/contracts/desiredstate"
)

var ErrUnpublished = errors.New("confirmed inbound configuration missing")

func (g *Graph) Inbound(id string) (desiredstate.State, desiredstate.Inbound, error) {
	for _, state := range g.States {
		for _, inbound := range state.Inbounds {
			if inbound.ID == id {
				return state, inbound, nil
			}
		}
	}
	return desiredstate.State{}, desiredstate.Inbound{}, fmt.Errorf("subscription inbound %s: %w", id, ErrUnpublished)
}

func (g *Graph) Route(id string) ([]string, error) {
	state, inbound, err := g.Inbound(id)
	if err != nil {
		return nil, err
	}
	ids, err := g.inbound(state, inbound, map[string]bool{})
	return Unique(ids), err
}

func (g *Graph) inbound(state desiredstate.State, inbound desiredstate.Inbound, visiting map[string]bool) ([]string, error) {
	ref := state.NodeID + ":inbound:" + inbound.ID
	if visiting[ref] {
		return nil, fmt.Errorf("subscription route %s: confirmed configuration contains a loop", ref)
	}
	visiting[ref] = true
	defer delete(visiting, ref)
	ids := []string{state.NodeID}
	if f := inbound.Settings.Forward; f != nil {
		for _, hop := range f.Hops {
			next, err := g.gost(hop, visiting)
			if err != nil {
				return nil, fmt.Errorf("subscription route %s: %w", ref, err)
			}
			ids = append(ids, next...)
		}
	}
	next, err := g.outbound(state, inbound.DefaultOutbound, visiting)
	return append(ids, next...), err
}

func (g *Graph) outbound(state desiredstate.State, ref string, visiting map[string]bool) ([]string, error) {
	if ref == "" || ref == "direct" || ref == "block" {
		return nil, nil
	}
	key := state.NodeID + ":outbound:" + ref
	if visiting[key] {
		return nil, fmt.Errorf("subscription route %s: confirmed outbound loop", key)
	}
	visiting[key] = true
	defer delete(visiting, key)
	if strings.HasPrefix(ref, "chain:") {
		for _, chain := range state.ProxyChains {
			if chain.ID != strings.TrimPrefix(ref, "chain:") {
				continue
			}
			ids := append([]string(nil), chain.NodeIDs...)
			// Only the entry outbound is executed on this machine. Each receiving
			// listener's successful state proves the continuation to the next hop.
			if len(chain.OutboundIDs) == 0 {
				return nil, fmt.Errorf("subscription route %s: empty published chain", key)
			}
			next, err := g.outbound(state, chain.OutboundIDs[0], visiting)
			if err != nil {
				return nil, err
			}
			ids = append(ids, next...)
			next, err = g.backup(state, chain.Failover, chain.BackupTargetID, visiting)
			return append(ids, next...), err
		}
		return nil, fmt.Errorf("subscription route %s: published chain missing", key)
	}
	if strings.HasPrefix(ref, "group:") {
		for _, group := range state.OutboundGroups {
			if group.ID != strings.TrimPrefix(ref, "group:") {
				continue
			}
			var ids []string
			for _, member := range group.Members {
				next, err := g.outbound(state, member.OutboundID, visiting)
				if err != nil {
					return nil, err
				}
				ids = append(ids, next...)
			}
			next, err := g.backup(state, group.Failover, group.BackupTargetID, visiting)
			return append(ids, next...), err
		}
		return nil, fmt.Errorf("subscription route %s: published outbound group missing", key)
	}
	for _, out := range state.Outbounds {
		if out.ID != ref {
			continue
		}
		var ids []string
		targetPort := out.Port
		// A loopback socket is part of this same confirmed configuration, so
		// following it does not guess an external server's identity from its IP.
		if ip := net.ParseIP(out.Address); ip != nil && ip.IsLoopback() {
			found := false
			for _, in := range state.Inbounds {
				if in.Port != out.Port || in.Protocol != "forward" || !in.Enabled {
					continue
				}
				if found {
					return nil, fmt.Errorf("subscription route %s: ambiguous local forward", key)
				}
				found = true
				if in.Settings.Forward != nil {
					targetPort = in.Settings.Forward.Port
				}
				next, err := g.inbound(state, in, visiting)
				if err != nil {
					return nil, err
				}
				ids = append(ids, next...)
			}
		}
		if out.TargetNodeID != "" {
			target, ok := g.States[out.TargetNodeID]
			if !ok {
				return nil, fmt.Errorf("subscription route %s: target %s confirmed configuration missing", key, out.TargetNodeID)
			}
			var matched *desiredstate.Inbound
			for i, in := range target.Inbounds {
				if in.Protocol != out.Protocol || in.Port != targetPort || !in.Enabled || !slices.Contains(in.CredentialIDs, out.CredentialID) {
					continue
				}
				if matched != nil {
					return nil, fmt.Errorf("subscription route %s: target credential identifies multiple listeners", key)
				}
				matched = &target.Inbounds[i]
			}
			if matched == nil {
				return nil, fmt.Errorf("subscription route %s: target %s published credential/listener proof missing", key, out.TargetNodeID)
			}
			next, err := g.inbound(target, *matched, visiting)
			if err != nil {
				return nil, err
			}
			ids = append(ids, next...)
		}
		next, err := g.backup(state, out.FailurePolicy, out.BackupTargetID, visiting)
		return append(ids, next...), err
	}
	return nil, fmt.Errorf("subscription route %s: published outbound missing", key)
}

func (g *Graph) backup(state desiredstate.State, mode, id string, visiting map[string]bool) ([]string, error) {
	ref := id
	switch desiredstate.BackupKind(mode) {
	case "Outbound":
	case "OutboundGroup":
		ref = "group:" + id
	case "ProxyChain":
		ref = "chain:" + id
	default:
		return nil, nil
	}
	return g.outbound(state, ref, visiting)
}

// GostReceiver binds a hop to its receiving listener using the published TLS
// identity. Hostnames and public addresses are never used as managed node IDs.
func GostReceiver(hop desiredstate.GostHop, in desiredstate.Inbound) bool {
	f := in.Settings.Forward
	if !in.Enabled || in.Port != hop.Port || f == nil || f.Relay == nil || f.Relay.ClientCAPEM != hop.CACertPEM {
		return false
	}
	block, _ := pem.Decode([]byte(f.Relay.CertificatePEM))
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return err == nil && cert.VerifyHostname(hop.ServerName) == nil
}

func (g *Graph) gost(hop desiredstate.GostHop, visiting map[string]bool) ([]string, error) {
	var target desiredstate.State
	var listener desiredstate.Inbound
	found := false
	for _, state := range g.States {
		for _, in := range state.Inbounds {
			if !GostReceiver(hop, in) {
				continue
			}
			if found {
				return nil, fmt.Errorf("published GOST hop has ambiguous receiving identity")
			}
			found, target, listener = true, state, in
		}
	}
	if !found {
		return nil, fmt.Errorf("published GOST hop: confirmed receiving TLS identity missing")
	}
	return g.inbound(target, listener, visiting)
}
