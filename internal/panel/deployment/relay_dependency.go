package deployment

import (
	"crypto/ecdh"
	"encoding/base64"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"proxycontrol/pkg/contracts/desiredstate"
)

// Only fixed reason codes escape this function. Keys, peer routes and private
// protocol settings remain inside the decrypted snapshot comparison.
func relayDependency(outbound desiredstate.Outbound, target desiredstate.State) string {
	if outbound.TargetNodeID != "" && target.NodeID != outbound.TargetNodeID {
		return "target_node_mismatch"
	}
	reason := "internal_inbound_unavailable"
	for _, inbound := range target.Inbounds {
		if !inbound.Enabled || inbound.Protocol != outbound.Protocol || inbound.Port != outbound.Port || inbound.SubscriptionVisible {
			continue
		}
		reason = "internal_credential_missing"
		for _, credentialID := range inbound.CredentialIDs {
			if credentialID != outbound.CredentialID {
				continue
			}
			for _, credential := range target.Credentials {
				if credential.ID != credentialID || !credential.Internal || credential.Protocol != outbound.Protocol {
					continue
				}
				if outbound.Protocol != "wireguard" {
					return "matched"
				}
				matched := wireguardRelayDependency(outbound, inbound, credential)
				if matched == "matched" {
					return matched
				}
				reason = matched
			}
		}
	}
	return reason
}

func relayWireguardKey(secret string) ([]byte, bool) {
	decoded, err := base64.StdEncoding.DecodeString(secret)
	if err != nil || len(decoded) != 32 {
		return nil, false
	}
	key, err := ecdh.X25519().NewPrivateKey(decoded)
	if err != nil {
		return nil, false
	}
	return key.PublicKey().Bytes(), true
}

func wireguardRelayDependency(outbound desiredstate.Outbound, inbound desiredstate.Inbound, credential desiredstate.Credential) string {
	source, target := outbound.Settings.WireGuard, inbound.Settings.WireGuard
	if source == nil || target == nil || len(source.Peers) != 1 {
		return "wireguard_settings_missing"
	}
	listen, err := netip.ParseAddr(inbound.Listen)
	if err != nil || listen.IsLoopback() {
		return "wireguard_listener_unreachable"
	}
	serverKey, ok := relayWireguardKey(target.SecretKey)
	if !ok || base64.StdEncoding.EncodeToString(serverKey) != source.Peers[0].PublicKey {
		return "wireguard_server_key_mismatch"
	}
	sourceKey, ok := relayWireguardKey(source.SecretKey)
	if !ok {
		return "wireguard_source_identity_mismatch"
	}
	credentialKey, ok := relayWireguardKey(credential.Secret)
	if !ok || base64.StdEncoding.EncodeToString(sourceKey) != base64.StdEncoding.EncodeToString(credentialKey) {
		return "wireguard_source_identity_mismatch"
	}
	var publishedPeer *desiredstate.WireGuardPeer
	for i := range target.Peers {
		if target.Peers[i].CredentialID == outbound.CredentialID {
			if publishedPeer != nil {
				return "wireguard_source_identity_mismatch"
			}
			publishedPeer = &target.Peers[i]
		}
	}
	if publishedPeer == nil || publishedPeer.PublicKey != base64.StdEncoding.EncodeToString(sourceKey) {
		return "wireguard_source_identity_mismatch"
	}
	host, port, err := net.SplitHostPort(source.Peers[0].Endpoint)
	parsedPort, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || parsedPort != outbound.Port || !relayHostMatches(host, outbound.Address) {
		return "wireguard_endpoint_mismatch"
	}
	if len(source.Addresses) == 0 || len(source.Addresses) > 2 || len(source.Addresses) != len(publishedPeer.AllowedIPs) {
		return "wireguard_peer_addresses_mismatch"
	}
	addresses := map[netip.Prefix]bool{}
	families := map[bool]bool{}
	for _, value := range source.Addresses {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Is4In6() || prefix.Bits() != prefix.Addr().BitLen() || families[prefix.Addr().Is4()] {
			return "wireguard_peer_addresses_mismatch"
		}
		addresses[prefix] = true
		families[prefix.Addr().Is4()] = true
	}
	for _, value := range publishedPeer.AllowedIPs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !addresses[prefix] {
			return "wireguard_peer_addresses_mismatch"
		}
		delete(addresses, prefix)
	}
	if len(addresses) != 0 {
		return "wireguard_peer_addresses_mismatch"
	}
	// The first target interface in each family is its managed allocation pool.
	pools := map[bool]netip.Prefix{}
	serverAddresses := map[netip.Addr]bool{}
	for _, value := range target.Addresses {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, parseErr := netip.ParseAddr(value)
			if parseErr != nil {
				return "wireguard_address_pool_mismatch"
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if prefix.Addr().Is4In6() {
			return "wireguard_address_pool_mismatch"
		}
		family := prefix.Addr().Is4()
		if _, exists := pools[family]; !exists {
			pools[family] = prefix.Masked()
		}
		serverAddresses[prefix.Addr()] = true
	}
	for _, value := range source.Addresses {
		prefix, _ := netip.ParsePrefix(value)
		pool, exists := pools[prefix.Addr().Is4()]
		if !exists || !pool.Contains(prefix.Addr()) || serverAddresses[prefix.Addr()] {
			return "wireguard_address_pool_mismatch"
		}
	}
	return "matched"
}

func relayHostMatches(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	l, lerr := netip.ParseAddr(left)
	r, rerr := netip.ParseAddr(right)
	if lerr == nil && rerr == nil {
		return l.Unmap() == r.Unmap()
	}
	return strings.EqualFold(strings.TrimSuffix(left, "."), strings.TrimSuffix(right, "."))
}
