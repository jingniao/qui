package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
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
// reserves every enabled inbound's numeric port across all cores and forwards,
// independent of transport. The panel rejects early with a clear conflict.
func (r *Repository) validateInboundPorts(ctx context.Context, tx *sql.Tx, id string, in InboundInput) error {
	if !in.Enabled {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM inbounds WHERE node_id=? AND port=? AND enabled=1 AND id<>?", in.NodeID, in.Port, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var other string
		if err := rows.Scan(&other); err != nil {
			return err
		}
		// Same numeric port on a node is reserved by the Agent contract, so a
		// second listener fails at apply time; refuse it here instead.
		return fmt.Errorf("inbound port %d is already used by %s on this node", in.Port, other)
	}
	return rows.Err()
}
