package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// ErrCoreSelectionPending reports an inbound bound to a component whose add or
// remove task has not been settled by a fresh capability report yet.
var ErrCoreSelectionPending = errors.New("core selection pending")

func ValidateInboundCore(c *pb.Capabilities, name, protocol string) error {
	core := node.SelectedCore(c, name)
	if core == nil {
		if protocol == "forward" {
			return errors.New("node Agent/core does not advertise forward inbound support")
		}
		if name != "" {
			return errors.New("selected core is not reported enabled")
		}
		// Preserve unknown legacy reports; the Agent still validates at apply.
		if c.GetCoreName() == "" {
			return nil
		}
		return errors.New("primary core unavailable")
	}
	if name != "" && !c.MultiCoreSupported && name != c.CoreName {
		return errors.New("multi-core unsupported")
	}
	if err := ValidateCoreProtocol(core.Name, protocol, true); err != nil {
		return err
	}
	if protocol == "forward" {
		if !core.ForwardInboundSupported {
			return errors.New("selected core does not support forward inbounds")
		}
		return nil
	}
	if len(core.Protocols) > 0 {
		for _, p := range core.Protocols {
			if p != nil && p.Protocol == protocol && p.Inbound {
				return nil
			}
		}
		return errors.New("selected core does not advertise inbound protocol")
	}
	return nil
}

func ValidateOutboundCores(c *pb.Capabilities, protocol string) error {
	cores := node.EnabledCores(c)
	if len(cores) == 0 {
		return ValidateCoreProtocol(c.GetCoreName(), protocol, false)
	}
	for _, core := range cores {
		if ValidateCoreProtocol(core.Name, protocol, false) == nil {
			return nil
		}
	}
	return fmt.Errorf("protocol %s is not supported by the node's %s core", protocol, c.GetCoreName())
}

// Inbound listener ports are node-global: the Agent's DesiredState validation
// reserves every enabled inbound's numeric ports across all cores and forwards,
// independent of transport. A GOST forward can expand to a range, so the check
// reads the node's enabled inbounds with their decrypted settings and intersects
// the expanded intervals instead of comparing base ports.
func (r *Repository) validateInboundPorts(ctx context.Context, tx *sql.Tx, id string, in InboundInput) error {
	if !in.Enabled {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,port,encrypted_settings FROM inbounds WHERE node_id=? AND enabled=1 AND id<>?", in.NodeID, id)
	if err != nil {
		return err
	}
	type occupant struct {
		id       string
		port     int
		settings desiredstate.ProtocolSettings
	}
	items := []occupant{}
	for rows.Next() {
		var item occupant
		var encoded []byte
		if err := rows.Scan(&item.id, &item.port, &encoded); err != nil {
			rows.Close()
			return err
		}
		if item.settings, err = r.decode(encoded); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ports := map[int]bool{}
	for _, port := range desiredstate.ListenPorts(in.Port, in.Settings) {
		ports[port] = true
	}
	for _, item := range items {
		for _, occupied := range desiredstate.ListenPorts(item.port, item.settings) {
			// Same numeric port on a node is reserved by the Agent contract, so a
			// second listener fails at apply time; refuse it here instead.
			if ports[occupied] {
				return fmt.Errorf("inbound port %d is already used by %s on this node", occupied, item.id)
			}
		}
	}
	return nil
}

// ValidateInboundForwardOptions refuses the GOST-only forward options unless the
// selected core is a GOST core that reports the settings contract they belong to.
// The option rules themselves live in desiredstate, so the panel and the Agent
// cannot disagree about what is a GOST-only option.
func ValidateInboundForwardOptions(c *pb.Capabilities, name string, settings desiredstate.ProtocolSettings) error {
	if settings.Forward == nil || !settings.Forward.HasGostOptions() {
		return nil
	}
	core := node.SelectedCore(c, name)
	if core == nil {
		return errors.New("node Agent/core does not advertise forward inbound support")
	}
	if err := desiredstate.ValidateForwardCore(core.Name, settings); err != nil {
		return err
	}
	if core.GostContractVersion < desiredstate.GostPoolContractVersion {
		return errors.New("gost core does not report forward targets, ranges or limits yet")
	}
	return nil
}
