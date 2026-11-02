// Package gost renders and runs the pinned official GOST release as the third
// core kind. G1 supports basic forwards: one TCP and/or UDP service per forward
// inbound with a single target, no routing, no policy, no user credential.
package gost

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	"proxycontrol/pkg/contracts/desiredstate"
)

const (
	// CoreName is the component and adapter name of this core kind.
	CoreName = "gost"
	// ConfigFile is the YAML the Agent materializes from the revision's native
	// JSON; the revision itself always stores that JSON, never YAML.
	ConfigFile = "config.yaml"
	// MetricsSocket is the private unix socket the counters are read from. It
	// lives inside the runtime directory, so no TCP port and no public surface
	// is added.
	MetricsSocket = "metrics.sock"
)

// Config is the part of the GOST configuration this Agent owns. Every field is
// rendered from typed settings; nothing is passed through from panel input.
type Config struct {
	Services []Service `json:"services" yaml:"services"`
	Metrics  Metrics   `json:"metrics" yaml:"metrics"`
	Log      Log       `json:"log" yaml:"log"`
}

// Service is one GOST service: a listener, its handler and the target it
// forwards to.
type Service struct {
	Name      string    `json:"name" yaml:"name"`
	Addr      string    `json:"addr" yaml:"addr"`
	Handler   Handler   `json:"handler" yaml:"handler"`
	Listener  Listener  `json:"listener" yaml:"listener"`
	Forwarder Forwarder `json:"forwarder" yaml:"forwarder"`
}

// Handler is the protocol a service terminates; `tcp` and `udp` are the forward
// handlers of the pinned release.
type Handler struct {
	Type string `json:"type" yaml:"type"`
}

// Listener is the transport a service binds.
type Listener struct {
	Type string `json:"type" yaml:"type"`
}

// Forwarder carries the target of a forward service.
type Forwarder struct {
	Nodes []Node `json:"nodes" yaml:"nodes"`
}

// Node is one forward target. G2 adds pools and their selector.
type Node struct {
	Name string `json:"name" yaml:"name"`
	Addr string `json:"addr" yaml:"addr"`
}

// Metrics is the Prometheus endpoint. GOST accepts a bare socket path only as
// unix://, so the rendered address always carries the scheme.
type Metrics struct {
	Addr string `json:"addr" yaml:"addr"`
}

// Log is rendered at warn level: the default level logs several lines per
// forwarded connection, which is useless for a node and would flood journald.
type Log struct {
	Level  string `json:"level" yaml:"level"`
	Format string `json:"format" yaml:"format"`
}

// EncodeYAML materializes the configuration for the GOST process. The JSON form
// is the revision's native storage, so the two are always derived from the same
// typed value and can never disagree.
func EncodeYAML(config Config) ([]byte, error) {
	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// serviceKey is the part of a rendered service that GOST's own normalization
// must preserve; it is what the syntax gate compares after `-O json`. `log` is
// compared separately because GOST never prints it.
type serviceKey struct {
	Name     string
	Addr     string
	Handler  string
	Listener string
	Nodes    []string
}

func (c Config) keys() []serviceKey {
	keys := make([]serviceKey, 0, len(c.Services))
	for _, service := range c.Services {
		key := serviceKey{Name: service.Name, Addr: service.Addr, Handler: service.Handler.Type, Listener: service.Listener.Type}
		for _, node := range service.Forwarder.Nodes {
			key.Nodes = append(key.Nodes, node.Addr)
		}
		keys = append(keys, key)
	}
	return keys
}

// decodeKeys reads the `services` array of a `gost -C <file> -O json` answer.
// Unknown keys are ignored: only the fields the renderer set are compared.
func decodeKeys(data []byte) ([]serviceKey, error) {
	var document struct {
		Services []struct {
			Name    string `json:"name"`
			Addr    string `json:"addr"`
			Handler struct {
				Type string `json:"type"`
			} `json:"handler"`
			Listener struct {
				Type string `json:"type"`
			} `json:"listener"`
			Forwarder struct {
				Nodes []struct {
					Addr string `json:"addr"`
				} `json:"nodes"`
			} `json:"forwarder"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, errors.New("gost normalization output is not JSON")
	}
	keys := make([]serviceKey, 0, len(document.Services))
	for _, service := range document.Services {
		key := serviceKey{Name: service.Name, Addr: service.Addr, Handler: service.Handler.Type, Listener: service.Listener.Type}
		for _, node := range service.Forwarder.Nodes {
			key.Nodes = append(key.Nodes, node.Addr)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// compareKeys checks that GOST's own normalization kept the rendered services in
// order and unchanged. The names it reports are what the panel shows when an apply
// fails, so the first difference is named.
func compareKeys(rendered, normalized []serviceKey) error {
	if len(rendered) != len(normalized) {
		return fmt.Errorf("gost normalization returned %d services, the renderer produced %d", len(normalized), len(rendered))
	}
	for index := range rendered {
		if rendered[index].String() != normalized[index].String() {
			return fmt.Errorf("gost normalization changed service %q to %q", rendered[index].String(), normalized[index].String())
		}
	}
	return nil
}

func (k serviceKey) String() string {
	return k.Name + " " + k.Addr + " " + k.Handler + "/" + k.Listener + " -> " + strings.Join(k.Nodes, ",")
}

// networkTypes maps a forward network setting to the transports it renders.
func networkTypes(network string) []string {
	switch network {
	case "tcp":
		return []string{"tcp"}
	case "udp":
		return []string{"udp"}
	case "tcp,udp":
		return []string{"tcp", "udp"}
	default:
		return nil
	}
}

// forwardNodes is the single legacy target of one forward inbound. G2 replaces
// it with the rendered pool, and raises the reported contract version with it.
func forwardNodes(inbound desiredstate.Inbound) []Node {
	forward := inbound.Settings.Forward
	return []Node{{Name: inbound.ID, Addr: joinHostPort(forward.Host, forward.Port)}}
}
