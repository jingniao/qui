// Package gost renders and runs the pinned official GOST release as the third
// core kind. G1 supports basic forwards: one TCP and/or UDP service per forward
// inbound with a single target, no routing, no policy, no user credential.
package gost

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	Services  []Service `json:"services" yaml:"services"`
	Limiters  []Limiter `json:"limiters,omitempty" yaml:"limiters,omitempty"`
	Climiters []Limiter `json:"climiters,omitempty" yaml:"climiters,omitempty"`
	Metrics   Metrics   `json:"metrics" yaml:"metrics"`
	Log       Log       `json:"log" yaml:"log"`
}

// Service is one GOST service: a listener, its handler, the target pool it
// forwards to and the optional limiters that reference it.
type Service struct {
	Name      string    `json:"name" yaml:"name"`
	Addr      string    `json:"addr" yaml:"addr"`
	Handler   Handler   `json:"handler" yaml:"handler"`
	Listener  Listener  `json:"listener" yaml:"listener"`
	Limiter   string    `json:"limiter,omitempty" yaml:"limiter,omitempty"`
	Climiter  string    `json:"climiter,omitempty" yaml:"climiter,omitempty"`
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

// Forwarder carries the ordered target pool of a forward service and the selector
// that decides which member is dialled.
type Forwarder struct {
	Selector *Selector `json:"selector,omitempty" yaml:"selector,omitempty"`
	Nodes    []Node    `json:"nodes" yaml:"nodes"`
}

// Selector is the forwarder's member choice: `round` alternates, `fifo` stays on
// the first reachable member. maxFails and failTimeout mark a member down and say
// when it is tried again.
type Selector struct {
	Strategy    string `json:"strategy" yaml:"strategy"`
	MaxFails    int    `json:"maxFails" yaml:"maxFails"`
	FailTimeout string `json:"failTimeout" yaml:"failTimeout"`
}

// Node is one forward target.
type Node struct {
	Name string `json:"name" yaml:"name"`
	Addr string `json:"addr" yaml:"addr"`
}

// Limiter is one entry of `limiters` (bytes per second) or `climiters` (concurrent
// connections). Both share `{name, limits}` in the pinned release.
type Limiter struct {
	Name   string   `json:"name" yaml:"name"`
	Limits []string `json:"limits" yaml:"limits"`
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

// targetNodes renders the whole ordered pool. Names are positional and stable for
// one rendered pool; the address is the only identity GOST needs.
func targetNodes(forward *desiredstate.ForwardSettings) []Node {
	pool := forward.TargetPool()
	nodes := make([]Node, 0, len(pool))
	for index, target := range pool {
		nodes = append(nodes, Node{Name: "target-" + strconv.Itoa(index), Addr: joinHostPort(target.Host, target.Port)})
	}
	return nodes
}

// bandwidthLimiter renders the byte-rate limit of one service. GOST parses each
// value with a base-2 byte-unit parser that rejects a bare integer, so the one
// canonical encoding is the integer byte count with the `B` suffix; 0 is the value
// the parser special-cases and means "no limit in this direction".
func bandwidthLimiter(name string, forward *desiredstate.ForwardSettings) (string, string) {
	in, out := byteLimit(forward.BandwidthInBps), byteLimit(forward.BandwidthOutBps)
	if in == "0" && out == "0" {
		return "", ""
	}
	return "lim-" + name, "$ " + in + " " + out
}

func byteLimit(rate *int64) string {
	if rate == nil || *rate == 0 {
		return "0"
	}
	return strconv.FormatInt(*rate, 10) + "B"
}

// connectionLimiter renders the concurrent connection cap of one service. G0
// measured TCP concurrency only, so a tcp,udp forward caps its TCP service and
// validation refuses the field on a UDP-only forward.
func connectionLimiter(name, network string, forward *desiredstate.ForwardSettings) string {
	if forward.MaxConnections == nil || *forward.MaxConnections == 0 || network != "tcp" {
		return ""
	}
	return "clim-" + name
}

// validateReferences refuses typed output whose limiter references do not
// resolve. GOST silently ignores an unknown limiter name and then forwards
// without the limit that was asked for, so this is checked before the process
// ever sees the configuration.
func validateReferences(config Config) error {
	limiters, climiters := map[string]bool{}, map[string]bool{}
	for _, limiter := range config.Limiters {
		limiters[limiter.Name] = true
	}
	for _, limiter := range config.Climiters {
		climiters[limiter.Name] = true
	}
	for _, service := range config.Services {
		if len(service.Forwarder.Nodes) == 0 {
			return fmt.Errorf("gost service %s has no target", service.Name)
		}
		if service.Limiter != "" && !limiters[service.Limiter] {
			return fmt.Errorf("gost service %s references an unknown limiter", service.Name)
		}
		if service.Climiter != "" && !climiters[service.Climiter] {
			return fmt.Errorf("gost service %s references an unknown climiter", service.Name)
		}
	}
	return nil
}
