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

// Enabled saves conflict with enabled inbounds; disabled saves only with claims.
func (r *Repository) validateInboundPorts(ctx context.Context, tx *sql.Tx, id string, in InboundInput) error {
	mode := "none"
	if in.Enabled {
		mode = "enabled"
	}
	exclude := []PortOwner{{Kind: "inbound", ID: id}}
	var kind, owner string
	err := tx.QueryRowContext(ctx, "SELECT owner_kind,owner_id FROM tunnel_objects WHERE object_kind='inbound' AND object_id=?", id).Scan(&kind, &owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if owner != "" {
		exclude = append(exclude, PortOwner{Kind: kind, ID: owner})
	}
	occupied, err := r.portOccupants(ctx, tx, in.NodeID, exclude, mode)
	if err != nil {
		return err
	}
	for _, port := range desiredstate.ClaimedPorts(in.Port, in.Settings) {
		if owners := occupied[port]; len(owners) > 0 {
			return fmt.Errorf("inbound port %d is already used by %s on this node", port, owners[0].ID)
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

// ValidateInboundServerSettings refuses the self-hosted xhttp/Reality settings and
// node-side certificate files unless the selected core has explicitly reported the
// support they need (compatibility plan §13.3 item 3, v1.4.7 section 3). Legacy
// combinations need no report, so every existing inbound keeps saving exactly as
// before.
//
// The previous report is the only evidence accepted: an Agent that has not
// reported the core, an uninstalled core, version text, the VLESS protocol flag
// and another core's report all leave the gate closed. Its counterparts on the
// publish paths are the same call in proxy.Snapshot and
// deployment.validateStateCoresTx.
func ValidateInboundServerSettings(c *pb.Capabilities, name, protocol string, settings desiredstate.ProtocolSettings) error {
	// Transport/protocol pairing (v1.5.2 item 6): checked here, not in Decode, so
	// historical revisions stay readable.
	if err := desiredstate.ValidateInboundTransport(protocol, settings.Transport); err != nil {
		return err
	}
	if !settings.HasServerSettings() && !settings.HasCertificateFiles() {
		return nil
	}
	core := node.SelectedCore(c, name)
	if core == nil {
		return errors.New("the selected core is not reported enabled, so it cannot apply these settings")
	}
	return desiredstate.RequireInboundServerSettings(core.Name, node.SettingsContractCapability(core), protocol, settings)
}
