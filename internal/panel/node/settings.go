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
	ConnectHost       *string
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
//
// A public IP the Owner types is marked manual and is never replaced by the
// address an Agent reports. Clearing it drops the mark too, so the next
// heartbeat fills the address again.
func (r *Repository) UpdateSettings(ctx context.Context, id string, input SettingsInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 || input.PublicIP != nil && *input.PublicIP != "" && net.ParseIP(*input.PublicIP) == nil || input.ConnectHost != nil && !validConnectHost(strings.TrimSpace(*input.ConnectHost)) {
		return ErrSettings
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var allow, maintenance, wasEnabled, manual bool
	var publicIP, connectHost, deleted string
	var network desiredstate.Network
	if err := tx.QueryRowContext(ctx, "SELECT allow_subscription,maintenance,public_ip,public_ip_manual,connect_host,enabled,outbound_source_ip,outbound_interface,host_source_ipv4,host_source_ipv6,coalesce(deleted_at,'') FROM nodes WHERE id=?", id).Scan(&allow, &maintenance, &publicIP, &manual, &connectHost, &wasEnabled, &network.OutboundSourceIP, &network.OutboundInterface, &network.HostSourceIPv4, &network.HostSourceIPv6, &deleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// A deleted node cannot be enabled, renamed or pointed at another address.
	if deleted != "" {
		return ErrNodeDeleted
	}
	if input.AllowSubscription != nil {
		allow = *input.AllowSubscription
	}
	if input.Maintenance != nil {
		maintenance = *input.Maintenance
	}
	if input.PublicIP != nil {
		publicIP = *input.PublicIP
		manual = publicIP != ""
	}
	if input.ConnectHost != nil {
		connectHost = strings.TrimSpace(*input.ConnectHost)
	}
	if network, err = networkSettings(network, input); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET name=?,enabled=?,allow_subscription=?,maintenance=?,public_ip=?,public_ip_manual=?,connect_host=?,outbound_source_ip=?,outbound_interface=?,host_source_ipv4=?,host_source_ipv6=?,status=CASE WHEN ?=0 THEN 'disabled' WHEN enabled=0 THEN 'offline' ELSE status END WHERE id=?", input.Name, input.Enabled, allow, maintenance, publicIP, manual, connectHost, network.OutboundSourceIP, network.OutboundInterface, network.HostSourceIPv4, network.HostSourceIPv6, input.Enabled, id); err != nil {
		return err
	}
	return tx.Commit()
}
