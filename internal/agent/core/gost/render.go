package gost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// validateDeadline bounds the syntax gate. GOST has no validation mode; `-C
// <file> -O json` only parses and re-prints, so this gate catches a broken
// render and nothing semantic.
const validateDeadline = 10 * time.Second

// maxServices bounds the production services one node renders. A port range
// multiplies every forward by its ports and by its transports, so the bound is
// applied to the typed settings before anything is expanded.
const maxServices = 1024

// Render builds the native configuration and the runtime metadata of one desired
// state. Only forward inbounds can be assigned to this core. Each enabled forward
// renders one TCP and/or UDP service per listening port, named deterministically,
// with its whole ordered target pool.
func (a *Adapter) Render(ctx context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	if err := state.Validate(); err != nil {
		return core.RenderedConfig{}, err
	}
	// GOST has no routing, policy, chain or egress-group surface and no account.
	// A state that carries one of them must fail here instead of rendering a
	// forward that silently ignores it.
	if len(state.Credentials) > 0 || len(state.Policies) > 0 || len(state.ProxyChains) > 0 || len(state.OutboundGroups) > 0 {
		return core.RenderedConfig{}, errors.New("gost forwards carry no credentials, policy, proxy chain or egress group")
	}
	if a.Directory == "" {
		return core.RenderedConfig{}, errors.New("gost adapter requires a runtime directory")
	}
	outbounds := map[string]desiredstate.Outbound{}
	for _, outbound := range state.Outbounds {
		outbounds[outbound.ID] = outbound
	}
	total := 0
	for _, inbound := range state.Inbounds {
		if inbound.Core != "" && inbound.Core != CoreName {
			return core.RenderedConfig{}, errors.New("inbound assigned to another core")
		}
		if !inbound.Enabled {
			continue
		}
		if inbound.Protocol != "forward" || inbound.Settings.Forward == nil {
			return core.RenderedConfig{}, fmt.Errorf("gost inbound %s is not a forward", inbound.ID)
		}
		// A forward leaves through its own target, so the only outbound it may
		// name is the implicit direct one. G3 adds the relay route.
		if outbound, ok := outbounds[inbound.DefaultOutbound]; !ok || !outbound.Enabled || outbound.Protocol != "direct" {
			return core.RenderedConfig{}, fmt.Errorf("gost forward %s requires an enabled direct default outbound", inbound.ID)
		}
		total += len(desiredstate.ListenPorts(inbound.Port, inbound.Settings)) * len(networkTypes(inbound.Settings.Forward.Network))
		if inbound.Settings.Forward.RelayMode() {
			// A relay listener is one service regardless of its (absent) network.
			total++
		}
	}
	if total > maxServices {
		return core.RenderedConfig{}, fmt.Errorf("a gost node renders at most %d services, this revision needs %d", maxServices, total)
	}
	config := Config{
		Services: []Service{},
		Metrics:  Metrics{Addr: "unix://" + filepath.Join(a.Directory, MetricsSocket)},
		Log:      Log{Level: "warn", Format: "text"},
	}
	rendered := core.RenderedConfig{NodeID: state.NodeID, Revision: state.Revision}
	for _, inbound := range state.Inbounds {
		if !inbound.Enabled {
			continue
		}
		forward := inbound.Settings.Forward
		// Ports are expanded here: GOST has no range syntax in `addr`, so a range
		// is one service per port, each with the whole pool and its own limits.
		for _, port := range desiredstate.ListenPorts(inbound.Port, inbound.Settings) {
			if forward.RelayMode() {
				// The exit half is one TLS relay listener; it forwards nothing
				// itself, so there is no target and no traffic counter of it.
				service, err := a.relayService(inbound, port)
				if err != nil {
					return core.RenderedConfig{}, err
				}
				config.Services = append(config.Services, service)
				rendered.HealthEndpoints = append(rendered.HealthEndpoints, joinHostPort(dialHost(inbound.Listen), port))
				continue
			}
			chain, err := a.chainFor(inbound, forward)
			if err != nil {
				return core.RenderedConfig{}, err
			}
			if chain != nil {
				config.Chains = append(config.Chains, *chain)
			}
			for _, network := range networkTypes(forward.Network) {
				service := forwardService(inbound, port, network)
				if chain != nil {
					service.Handler.Chain = chain.Name
				}
				if name, limit := bandwidthLimiter(service.Name, forward); name != "" {
					service.Limiter = name
					config.Limiters = append(config.Limiters, Limiter{Name: name, Limits: []string{limit}})
				}
				if name := connectionLimiter(service.Name, network, forward); name != "" {
					service.Climiter = name
					config.Climiters = append(config.Climiters, Limiter{Name: name, Limits: []string{"$ " + strconv.Itoa(*forward.MaxConnections)}})
				}
				config.Services = append(config.Services, service)
				dial := joinHostPort(dialHost(inbound.Listen), port)
				if network == "udp" {
					rendered.UDPHealthEndpoints = append(rendered.UDPHealthEndpoints, dial)
				} else {
					rendered.HealthEndpoints = append(rendered.HealthEndpoints, dial)
				}
			}
		}
	}
	if err := validateReferences(config); err != nil {
		return core.RenderedConfig{}, err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	rendered.Data = data
	return rendered, nil
}

// forwardService is one native service of a forward: one listener, its transport
// and the pool the forwarder dials.
func forwardService(inbound desiredstate.Inbound, port int, network string) Service {
	forward := inbound.Settings.Forward
	strategy, maxFails, failTimeout := forward.Selector()
	return Service{
		Name:      serviceName(inbound.ID, port, network),
		Addr:      joinHostPort(inbound.Listen, port),
		Handler:   Handler{Type: network},
		Listener:  Listener{Type: network},
		Forwarder: &Forwarder{Selector: &Selector{Strategy: strategy, MaxFails: maxFails, FailTimeout: failTimeout}, Nodes: targetNodes(forward)},
	}
}

// serviceName is the GOST registry name of one forward service. It is also the
// metrics `service` label, so the counter filter and the panel's error messages
// name the same object. The inbound ID is already a safe component
// ([A-Za-z0-9-_.], never a path), so the name is deterministic and stable.
func serviceName(inboundID string, port int, network string) string {
	return "fwd-" + inboundID + "-" + strconv.Itoa(port) + "-" + network
}

// joinHostPort renders an address the way GOST and net.Listen expect, including
// the brackets an IPv6 literal needs.
func joinHostPort(host string, port int) string { return net.JoinHostPort(host, strconv.Itoa(port)) }

// dialHost maps a wildcard listener to a loopback address the Agent can dial or
// bind-probe; a specific listener address is used as it is.
func dialHost(listen string) string {
	switch listen {
	case "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	default:
		return listen
	}
}

// Validate is the first of the two stages GOST needs. It writes the candidate to a
// private file and requires `gost -C <file> -O json` to parse it and to keep every
// rendered service. It never touches the running process: GOST rejects unknown
// handlers, duplicate names and occupied ports only when it serves, which is what
// the start-and-settle gate after the switch is for.
func (a *Adapter) Validate(ctx context.Context, configuration core.RenderedConfig) error {
	var config Config
	if err := json.Unmarshal(configuration.Data, &config); err != nil {
		return errors.New("invalid gost native configuration")
	}
	data, err := EncodeYAML(config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.Directory, 0700); err != nil {
		return err
	}
	path, err := writePrivate(a.Directory, ".validate-*.yaml", data)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	deadline, cancel := context.WithTimeout(ctx, validateDeadline)
	defer cancel()
	command := exec.CommandContext(deadline, a.Path, "-C", path, "-O", "json")
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return fmt.Errorf("gost rejected the rendered configuration: %w: %s", err, output.reason())
	}
	keys, err := decodeKeys(output.Bytes())
	if err != nil {
		return err
	}
	if err := compareKeys(config.keys(), keys); err != nil {
		return err
	}
	return nil
}

// boundedOutput keeps the first part of a core's output. A fatal message is short;
// the cap keeps a runaway core from filling the Agent's memory.
type boundedOutput struct {
	data []byte
}

const boundedOutputLimit = 8 << 10

func (o *boundedOutput) Write(data []byte) (int, error) {
	if room := boundedOutputLimit - len(o.data); room > 0 {
		if len(data) > room {
			o.data = append(o.data, data[:room]...)
		} else {
			o.data = append(o.data, data...)
		}
	}
	return len(data), nil
}

func (o *boundedOutput) Bytes() []byte { return o.data }

// reason is the first line GOST printed, which is its fatal message when the
// configuration was refused.
func (o *boundedOutput) reason() string {
	for index, b := range o.data {
		if b == '\n' {
			return string(o.data[:index])
		}
	}
	return string(o.data)
}

var _ io.Writer = (*boundedOutput)(nil)
