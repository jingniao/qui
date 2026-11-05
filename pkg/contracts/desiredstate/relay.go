package desiredstate

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"strconv"
	"strings"
)

// GostForwardModeRelay marks a forward inbound as a receiving half of a GOST
// encrypted relay instead of a port forward. The node renders a `relay` handler
// on a TLS listener that requires a client certificate; the entry node renders
// the same tunnel as an ordered hop chain under its forward service's
// `handler.chain`.
const GostForwardModeRelay = "relay"

// GostMaxRelayHops preserves the contract-3 entry-chain limit. Contract 4 adds
// a middle listener with just one next hop, not a longer native entry chain.
const GostMaxRelayHops = 2

// GostRelayTLS is a receiving half of a GOST tunnel: the certificate the relay
// listener presents and the CA that must have issued the peer certificate. The
// panel resolves it from the tunnel's sealed material; the public inbound API
// never accepts it.
type GostRelayTLS struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	ClientCAPEM    string `json:"client_ca_pem"`
}

// GostHop is one ordered relay an entry forward dials its target through: the
// relay's address, the server name its certificate carries, the CA that must
// have signed it and the client certificate this node presents.
type GostHop struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	ServerName    string `json:"server_name"`
	CACertPEM     string `json:"ca_cert_pem"`
	ClientCertPEM string `json:"client_cert_pem"`
	ClientKeyPEM  string `json:"client_key_pem"`
}

// Addr is the relay address in the form GOST and net.Dial expect.
func (h GostHop) Addr() string { return net.JoinHostPort(h.Host, strconv.Itoa(h.Port)) }

// validRelayTLS requires a usable key pair and a CA certificate. The Agent is the
// final authority, but a state that cannot possibly serve is refused before it
// ever reaches a node.
func validRelayTLS(material GostRelayTLS) error {
	if _, err := tls.X509KeyPair([]byte(material.CertificatePEM), []byte(material.PrivateKeyPEM)); err != nil {
		return errors.New("relay listener requires a usable TLS certificate and key")
	}
	return validCertificatePEM(material.ClientCAPEM)
}

func validClientMaterial(ca, certificate, key string) error {
	if err := validCertificatePEM(ca); err != nil {
		return err
	}
	if _, err := tls.X509KeyPair([]byte(certificate), []byte(key)); err != nil {
		return errors.New("each gost hop requires a usable client certificate and key")
	}
	return nil
}

// validCertificatePEM accepts a PEM bundle crypto/tls could use as a root pool and
// nothing else, so a truncated or unrelated blob is refused.
func validCertificatePEM(data string) error {
	if strings.TrimSpace(data) == "" {
		return errors.New("ca certificate required")
	}
	if !x509.NewCertPool().AppendCertsFromPEM([]byte(data)) {
		return errors.New("invalid ca certificate")
	}
	for rest := []byte(data); len(strings.TrimSpace(string(rest))) > 0; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return errors.New("invalid ca certificate")
		}
	}
	return nil
}

// validateGostRelay validates a receiving half: a TLS relay with no target pool,
// optionally dialing one next relay. An entry forward keeps its pool and is
// validated by Validate plus validateGostHops.
func validateGostRelay(f *ForwardSettings) error {
	if f.Host != "" || f.Port != 0 || len(f.Targets) > 0 || f.Network != "" {
		return errors.New("a relay listener has no target pool and no network")
	}
	if f.PortEnd != nil || f.BandwidthInBps != nil || f.BandwidthOutBps != nil || f.MaxConnections != nil || f.Strategy != nil || f.MaxFails != nil || f.FailTimeout != nil {
		return errors.New("a relay listener has no forward pool options")
	}
	if len(f.Hops) > 1 {
		return errors.New("a middle relay listener dials exactly one next relay")
	}
	if len(f.Hops) == 1 {
		if err := validateGostHops(f.Hops); err != nil {
			return err
		}
	}
	if f.Relay == nil {
		return errors.New("a relay listener requires its TLS material")
	}
	return validRelayTLS(*f.Relay)
}

// validateGostHops validates the chain of an entry forward: one or two complete
// hops, each with the relay address, its certificate name and this node's client
// certificate.
func validateGostHops(hops []GostHop) error {
	if len(hops) == 0 || len(hops) > GostMaxRelayHops {
		return errors.New("a gost forward dials through one or two relays")
	}
	for _, hop := range hops {
		if !validForwardHost(hop.Host) || hop.Port < 1 || hop.Port > 65535 {
			return errors.New("each gost hop requires a relay host and port")
		}
		if hop.ServerName == "" || len(hop.ServerName) > 253 {
			return errors.New("each gost hop requires the relay server name")
		}
		if err := validClientMaterial(hop.CACertPEM, hop.ClientCertPEM, hop.ClientKeyPEM); err != nil {
			return err
		}
	}
	return nil
}

// RelayMode reports whether the settings are a middle or exit relay listener.
func (f *ForwardSettings) RelayMode() bool { return f != nil && f.Mode == GostForwardModeRelay }

// ValidateGostContract checks a reported Agent capability before delivery. X1's
// tunnel planner must check every receiving node with this shared guard; a core
// version string is not a substitute for the negotiated contract.
func (f *ForwardSettings) ValidateGostContract(version uint32) error {
	if f.RelayMode() && len(f.Hops) > 0 && version < GostMiddleContractVersion {
		return errors.New("gost middle relay requires Agent gost_contract_version >= 4")
	}
	if f.HasRelaySettings() && version < GostRelayContractVersion {
		return errors.New("gost relay requires Agent gost_contract_version >= 3")
	}
	if f.HasGostOptions() && version < GostPoolContractVersion {
		return errors.New("gost forward options require Agent gost_contract_version >= 2")
	}
	return nil
}

// HasRelaySettings reports whether any field of the version 3 contract is set.
// Every layer that refuses relay material from an untrusted source asks this.
func (f *ForwardSettings) HasRelaySettings() bool {
	return f != nil && (f.Mode != "" || f.Relay != nil || len(f.Hops) > 0)
}
