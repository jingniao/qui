package proxy

import (
	"context"
	"errors"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
)

type AccessReader interface {
	AccessForGroups(context.Context, []string) ([]user.Access, error)
}

func snapshotList[T any](read func(int, int) ([]T, int, error)) ([]T, error) {
	all := []T{}
	expectedTotal := -1
	for page := 1; ; page++ {
		items, total, err := read(page, 100)
		if err != nil {
			return nil, err
		}
		if expectedTotal == -1 {
			expectedTotal = total
		} else if total != expectedTotal {
			return nil, errors.New("proxy configuration changed while creating snapshot; retry")
		}
		all = append(all, items...)
		if len(all) == total {
			return all, nil
		}
		if len(items) == 0 || len(all) > total {
			return nil, errors.New("inconsistent proxy configuration pagination")
		}
	}
}

func (s *Service) Snapshot(ctx context.Context, nodeID string, users AccessReader) (desiredstate.State, error) {
	n, err := s.Nodes.Get(ctx, nodeID)
	if err != nil || !n.Enabled {
		return desiredstate.State{}, errors.New("node unavailable")
	}
	groups, err := s.Nodes.SubscriptionGroups(ctx, nodeID)
	if err != nil {
		return desiredstate.State{}, err
	}
	access, err := users.AccessForGroups(ctx, groups)
	if err != nil {
		return desiredstate.State{}, err
	}
	inbounds, err := snapshotList(func(page, size int) ([]Inbound, int, error) {
		return s.Repository.Inbounds(ctx, nodeID, page, size, true)
	})
	if err != nil {
		return desiredstate.State{}, err
	}
	outbounds, err := snapshotList(func(page, size int) ([]Outbound, int, error) {
		return s.Repository.Outbounds(ctx, nodeID, page, size, true)
	})
	if err != nil {
		return desiredstate.State{}, err
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: nodeID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{}, Credentials: []desiredstate.Credential{}, Inbounds: []desiredstate.Inbound{}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}, OutboundGroups: []desiredstate.OutboundGroup{}, ProxyChains: []desiredstate.ProxyChain{}, Policies: []desiredstate.Policy{}}
	// The node's outbound network settings travel with every published state;
	// a node that configures none keeps the field absent.
	state.Network = desiredstate.NetworkOrNil(n.OutboundSourceIP, n.OutboundInterface, n.HostSourceIPv4, n.HostSourceIPv6)
	credentialSeen := map[string]bool{}
	add := func(credential desiredstate.Credential) {
		if !credentialSeen[credential.ID] {
			state.Credentials = append(state.Credentials, credential)
			credentialSeen[credential.ID] = true
		}
	}
	for _, entry := range access {
		state.Users = append(state.Users, desiredstate.User{ID: entry.User.ID, Enabled: true, ExpiresAt: entry.User.ExpiresAt})
	}
	configuredGroups, err := snapshotList(func(page, size int) ([]Group, int, error) {
		return s.Repository.Groups(ctx, nodeID, page, size)
	})
	if err != nil {
		return state, err
	}
	for _, group := range configuredGroups {
		if group.Enabled {
			if err := s.validateSavedGroup(ctx, group); err != nil {
				return state, err
			}
			state.OutboundGroups = append(state.OutboundGroups, group.Desired())
		}
	}
	for _, record := range outbounds {
		if !record.Enabled {
			continue
		}
		outbound := desiredstate.Outbound{ID: record.ID, Protocol: record.Protocol, Address: record.Address, Port: record.Port, Enabled: true, CredentialID: record.CredentialID, TargetNodeID: record.TargetNodeID, Settings: record.Settings, FailurePolicy: record.FailurePolicy, BackupTargetID: record.BackupOutboundID, SupportTCP: record.SupportTCP, SupportUDP: record.SupportUDP, ProbeURL: record.ProbeURL, ProbeInterval: record.ProbeInterval}
		if record.CredentialID != "" {
			credential, err := s.Repository.Credential(ctx, record.CredentialID)
			if err != nil {
				return state, err
			}
			add(credential)
		}
		state.Outbounds = append(state.Outbounds, outbound)
	}
	for _, record := range inbounds {
		inbound := desiredstate.Inbound{ID: record.ID, Protocol: record.Protocol, Listen: record.Listen, Port: record.Port, Enabled: record.Enabled, DefaultOutbound: record.DefaultOutbound, SubscriptionVisible: record.SubscriptionVisible, Settings: record.Settings, CredentialIDs: []string{}}
		if record.InternalRelay {
			if record.RelayCredentialID != "" {
				credential, err := s.Repository.Credential(ctx, record.RelayCredentialID)
				if err != nil {
					return state, err
				}
				add(credential)
				inbound.CredentialIDs = append(inbound.CredentialIDs, credential.ID)
			}
			bindings, err := s.Repository.Bindings(ctx, record.ID)
			if err != nil {
				return state, err
			}
			if inbound.Protocol == "wireguard" {
				copy := *inbound.Settings.WireGuard
				copy.Peers = append([]desiredstate.WireGuardPeer(nil), copy.Peers...)
				inbound.Settings.WireGuard = &copy
			}
			for _, binding := range bindings {
				credential, err := s.Repository.Credential(ctx, binding.CredentialID)
				if err != nil {
					return state, err
				}
				add(credential)
				inbound.CredentialIDs = append(inbound.CredentialIDs, credential.ID)
				if inbound.Protocol == "wireguard" {
					public, err := publicKey(credential.Secret)
					if err != nil {
						return state, err
					}
					inbound.Settings.WireGuard.Peers = append(inbound.Settings.WireGuard.Peers, desiredstate.WireGuardPeer{PublicKey: public, AllowedIPs: binding.TunnelAddresses, CredentialID: credential.ID})
				}
			}
			inbound.SubscriptionVisible = false
		} else {
			if inbound.Protocol == "wireguard" {
				copy := *inbound.Settings.WireGuard
				copy.Peers = append([]desiredstate.WireGuardPeer(nil), copy.Peers...)
				inbound.Settings.WireGuard = &copy
			}
			allowed := map[string]bool{}
			for _, id := range record.AllowedUserIDs {
				allowed[id] = true
			}
			for _, entry := range access {
				if len(allowed) > 0 && !allowed[entry.User.ID] {
					continue
				}
				for _, credential := range entry.Credentials {
					if credential.Protocol != inbound.Protocol {
						continue
					}
					desired := desiredstate.Credential{ID: credential.ID, Protocol: credential.Protocol, UserID: credential.UserID, DeviceID: credential.DeviceID, Username: credential.Username, Secret: credential.Secret}
					if credential.Protocol == "wireguard" {
						desired.Secret = ""
						desired.PublicKey = credential.PublicKey
						addresses, err := s.Repository.DeviceAddresses(ctx, record.ID, credential.DeviceID, record.Settings)
						if err != nil {
							return state, err
						}
						inbound.Settings.WireGuard.Peers = append(inbound.Settings.WireGuard.Peers, desiredstate.WireGuardPeer{PublicKey: credential.PublicKey, AllowedIPs: addresses, CredentialID: credential.ID})
					}
					add(desired)
					inbound.CredentialIDs = append(inbound.CredentialIDs, credential.ID)
				}
			}
		}
		if len(inbound.CredentialIDs) == 0 {
			inbound.Enabled = false
		}
		state.Inbounds = append(state.Inbounds, inbound)
	}
	if s.Policies != nil {
		rules, err := s.Policies.ForNode(ctx, nodeID)
		if err != nil {
			return state, err
		}
		state.Policies = rules
	}
	chains, err := snapshotList(func(page, size int) ([]Chain, int, error) {
		return s.Repository.Chains(ctx, nodeID, page, size)
	})
	if err != nil {
		return state, err
	}
	for _, chain := range chains {
		if !chain.Enabled {
			continue
		}
		if _, err := s.validateChain(ctx, chain.ID, chainInput(chain)); err != nil {
			return state, err
		}
		state.ProxyChains = append(state.ProxyChains, chain.Desired())
		for index, hop := range chain.Hops {
			if hop.TargetNodeID != nodeID {
				continue
			}
			next := chain.FinalOutboundID
			if index+1 < len(chain.Hops) {
				next = chain.Hops[index+1].OutboundID
			}
			for position := range state.Inbounds {
				if state.Inbounds[position].ID == hop.TargetInboundID {
					// Keep the chain's data and end-to-end health probe on the
					// declared path. A standalone hop's Direct fallback must not
					// silently turn this chain into a different exit.
					continuation := next
					for _, outbound := range state.Outbounds {
						if outbound.ID == next && outbound.Protocol != "direct" && outbound.Protocol != "block" {
							copy := outbound
							copy.ID = "_chain_continue_" + chain.ID
							copy.FailurePolicy = "Block"
							copy.BackupTargetID = ""
							state.Outbounds = append(state.Outbounds, copy)
							continuation = copy.ID
							break
						}
					}
					state.Inbounds[position].DefaultOutbound = continuation
				}
			}
		}
	}
	return state, state.Validate()
}
