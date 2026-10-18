package node

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"

	"proxycontrol/pkg/contracts/desiredstate"
)

var ErrSettings = errors.New("invalid node settings")

type SettingsInput struct {
	Name              string
	Enabled           bool
	AllowSubscription *bool
	Maintenance       *bool
	PublicIP          *string
	OutboundSourceIP  *string
	OutboundInterface *string
	HostSourceIPv4    *string
	HostSourceIPv6    *string
}

// networkSettings merges the optional outbound-network overrides carried by the
// input with the stored values and validates the result with the same contract
// the Agent enforces. The HTTP handler calls it with empty stored values to
// reject invalid input before the repository runs.
func networkSettings(stored desiredstate.Network, input SettingsInput) (desiredstate.Network, error) {
	if input.OutboundSourceIP != nil {
		stored.OutboundSourceIP = strings.TrimSpace(*input.OutboundSourceIP)
	}
	if input.OutboundInterface != nil {
		stored.OutboundInterface = strings.TrimSpace(*input.OutboundInterface)
	}
	if input.HostSourceIPv4 != nil {
		stored.HostSourceIPv4 = strings.TrimSpace(*input.HostSourceIPv4)
	}
	if input.HostSourceIPv6 != nil {
		stored.HostSourceIPv6 = strings.TrimSpace(*input.HostSourceIPv6)
	}
	if err := stored.Validate(); err != nil {
		return desiredstate.Network{}, ErrSettings
	}
	return stored, nil
}

// Optional fields are merged with the transaction's current values, not a
// stale HTTP read. Every field commits together, including enabled/status.
func (r *Repository) UpdateSettings(ctx context.Context, id string, input SettingsInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 || input.PublicIP != nil && *input.PublicIP != "" && net.ParseIP(*input.PublicIP) == nil {
		return ErrSettings
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var allow, maintenance, wasEnabled bool
	var publicIP string
	var network desiredstate.Network
	if err := tx.QueryRowContext(ctx, "SELECT allow_subscription,maintenance,public_ip,enabled,outbound_source_ip,outbound_interface,host_source_ipv4,host_source_ipv6 FROM nodes WHERE id=?", id).Scan(&allow, &maintenance, &publicIP, &wasEnabled, &network.OutboundSourceIP, &network.OutboundInterface, &network.HostSourceIPv4, &network.HostSourceIPv6); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if input.AllowSubscription != nil {
		allow = *input.AllowSubscription
	}
	if input.Maintenance != nil {
		maintenance = *input.Maintenance
	}
	if input.PublicIP != nil {
		publicIP = *input.PublicIP
	}
	if network, err = networkSettings(network, input); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET name=?,enabled=?,allow_subscription=?,maintenance=?,public_ip=?,outbound_source_ip=?,outbound_interface=?,host_source_ipv4=?,host_source_ipv6=?,status=CASE WHEN ?=0 THEN 'disabled' WHEN enabled=0 THEN 'offline' ELSE status END WHERE id=?", input.Name, input.Enabled, allow, maintenance, publicIP, network.OutboundSourceIP, network.OutboundInterface, network.HostSourceIPv4, network.HostSourceIPv6, input.Enabled, id); err != nil {
		return err
	}
	return tx.Commit()
}
