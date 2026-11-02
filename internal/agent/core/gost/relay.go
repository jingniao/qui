package gost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"proxycontrol/pkg/contracts/desiredstate"
)

// The pinned release's relay building blocks (G0 section 6): a `relay` handler on
// a `tls` listener with a client CA is what makes the peer certificate
// mandatory, and a chain attaches to the handler of the forwarding service with
// `relay` connectors and `tls` dialers. The listener type `mtls` is *multiplexed*
// TLS, not mutual TLS, so it is never rendered.
const (
	relayHandler    = "relay"
	tlsListener     = "tls"
	relayConnector  = "relay"
	materialDir     = "tls"
	materialDirMode = 0700
)

// relayService renders the exit half of one GOST tunnel: a relay handler on a TLS
// listener that requires the client certificate of the entry node, with no target
// pool. Its bytes repeat the forwarded payload with TLS framing on top, so the
// service is excluded from the node's traffic totals (see forwardServiceNames).
func (a *Adapter) relayService(inbound desiredstate.Inbound, port int) (Service, error) {
	forward := inbound.Settings.Forward
	if forward == nil || forward.Relay == nil || forward.Relay.CertificatePEM == "" || forward.Relay.PrivateKeyPEM == "" || forward.Relay.ClientCAPEM == "" {
		return Service{}, fmt.Errorf("gost relay %s has incomplete TLS material", inbound.ID)
	}
	paths, err := a.writeMaterial(inbound.ID, "relay", forward.Relay.CertificatePEM, forward.Relay.PrivateKeyPEM, forward.Relay.ClientCAPEM)
	if err != nil {
		return Service{}, err
	}
	return Service{
		Name:     relayServiceName(inbound.ID, port),
		Addr:     joinHostPort(inbound.Listen, port),
		Handler:  Handler{Type: relayHandler},
		Listener: Listener{Type: tlsListener, TLS: paths},
	}, nil
}

// chainFor renders the entry half: the ordered relays one forward dials its
// target through, as one chain attached to the service handler. A forward
// without hops has no chain at all rather than an empty one.
func (a *Adapter) chainFor(inbound desiredstate.Inbound, forward *desiredstate.ForwardSettings) (*Chain, error) {
	if len(forward.Hops) == 0 {
		return nil, nil
	}
	if len(forward.Hops) > desiredstate.GostMaxRelayHops {
		return nil, errors.New("a gost forward dials through one or two relays")
	}
	chain := &Chain{Name: chainName(inbound.ID), Hops: make([]Hop, 0, len(forward.Hops))}
	for index, hop := range forward.Hops {
		paths, err := a.writeMaterial(inbound.ID, "hop"+strconv.Itoa(index), hop.ClientCertPEM, hop.ClientKeyPEM, hop.CACertPEM)
		if err != nil {
			return nil, err
		}
		paths.ServerName = hop.ServerName
		chain.Hops = append(chain.Hops, Hop{
			Name: "hop-" + strconv.Itoa(index),
			Nodes: []HopNode{{
				Name:      "relay-" + strconv.Itoa(index),
				Addr:      hop.Addr(),
				Connector: Connector{Type: relayConnector},
				Dialer:    Dialer{Type: tlsListener, TLS: paths},
			}},
		})
	}
	return chain, nil
}

// writeMaterial stores one certificate set under its own directory and returns
// the file names GOST reads. The files are 0600 and the directory 0700: the core
// runs as the Agent's user and nothing else needs to read them. Material is keyed
// by the inbound and the half that produced it, so a rollback to a retained
// revision finds its own files instead of the newest ones.
func (a *Adapter) writeMaterial(inboundID, half, certificate, key, ca string) (*TLS, error) {
	if !safeComponent(inboundID) || !safeComponent(half) {
		return nil, errors.New("gost tunnel material needs a safe object name")
	}
	directory := filepath.Join(a.Directory, materialDir, inboundID, half)
	if err := os.MkdirAll(directory, materialDirMode); err != nil {
		return nil, err
	}
	paths := &TLS{CertFile: filepath.Join(directory, "cert.pem"), KeyFile: filepath.Join(directory, "key.pem"), CAFile: filepath.Join(directory, "ca.pem")}
	for path, data := range map[string]string{paths.CertFile: certificate, paths.KeyFile: key, paths.CAFile: ca} {
		if err := writeMaterialFile(path, data); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// writeMaterialFile replaces one file in place. The content of one half never
// changes, so a rewrite writes the same bytes; the key is never logged.
func writeMaterialFile(path, data string) error {
	if strings.TrimSpace(data) == "" {
		return errors.New("gost tunnel material is incomplete")
	}
	temp, err := writePrivate(filepath.Dir(path), ".material-*", []byte(data))
	if err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}
	return nil
}

// safeComponent reports whether an object name may be used as a directory name.
// The panel generates both names, so this only refuses a corrupted revision.
func safeComponent(name string) bool {
	if name == "" || len(name) > 96 || name == "." || name == ".." {
		return false
	}
	for _, character := range name {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.') {
			return false
		}
	}
	return true
}

// relayServiceName is the registry name and metrics `service` label of one relay
// listener. It deliberately does not use the `fwd-` prefix of the counters.
func relayServiceName(inboundID string, port int) string {
	return "relay-" + inboundID + "-" + strconv.Itoa(port)
}

// chainName is the chain one forward inbound dials through.
func chainName(inboundID string) string { return "chain-" + inboundID }

// forwardServiceNames is the set of services whose bytes count towards the node's
// traffic total: the forward services only. A relay service carries the same
// payload once more plus TLS framing, so counting it would double the node usage
// (G0 section 6.3).
func forwardServiceNames(config Config) map[string]bool {
	names := map[string]bool{}
	for _, service := range config.Services {
		if service.Handler.Type == relayHandler {
			continue
		}
		names[service.Name] = true
	}
	return names
}
