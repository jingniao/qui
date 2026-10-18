package proxy

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"proxycontrol/pkg/contracts/desiredstate"
)

func ensureWireguardAddresses(ctx context.Context, tx *sql.Tx, inboundID string, settings desiredstate.ProtocolSettings, existing []string) ([]string, error) {
	pools, reserved, err := wireguardPools(settings)
	if err != nil {
		return nil, err
	}
	assigned, err := assignedWireguardAddresses(ctx, tx, inboundID)
	if err != nil {
		return nil, err
	}
	if err := validateWireguardAssignments(pools, reserved, assigned); err != nil {
		return nil, err
	}
	reserved = append(reserved, assigned...)
	result := append([]string(nil), existing...)
	families := map[bool]bool{}
	for _, value := range existing {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() != prefix.Addr().BitLen() || families[prefix.Addr().Is4()] {
			return nil, errors.New("invalid WireGuard address family assignment")
		}
		families[prefix.Addr().Is4()] = true
	}
	for _, pool := range pools {
		if families[pool.Addr().Is4()] {
			continue
		}
		address, err := availableWireguardAddress(ctx, pool, reserved)
		if err != nil {
			return nil, err
		}
		result = append(result, address)
		prefix, _ := netip.ParsePrefix(address)
		reserved = append(reserved, prefix)
	}
	return result, nil
}

func availableWireguardAddress(ctx context.Context, pool netip.Prefix, reserved []netip.Prefix) (string, error) {
	candidate := pool.Addr()
	if candidate.Is6() || pool.Bits() < 31 {
		candidate = candidate.Next()
	}
	// Jump over entire reserved ranges, including manually configured peers.
	for attempts := 0; attempts <= len(reserved)+1 && candidate.IsValid() && pool.Contains(candidate); attempts++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if candidate.Is4() && pool.Bits() < 31 && candidate == lastWireguardAddress(pool) {
			break
		}
		next := candidate
		blocked := false
		for _, prefix := range reserved {
			if prefix.Contains(candidate) {
				blocked = true
				end := lastWireguardAddress(prefix)
				if !end.Next().IsValid() {
					return "", errors.New("WireGuard tunnel address pool exhausted")
				}
				if end.Compare(next) >= 0 {
					next = end.Next()
				}
			}
		}
		if !blocked {
			return netip.PrefixFrom(candidate, candidate.BitLen()).String(), nil
		}
		candidate = next
	}
	return "", errors.New("WireGuard tunnel address pool exhausted")
}

func assignedWireguardAddresses(ctx context.Context, tx *sql.Tx, inboundID string) ([]netip.Prefix, error) {
	rows, err := tx.QueryContext(ctx, "SELECT address,additional_address FROM wireguard_addresses WHERE inbound_id=? UNION ALL SELECT tunnel_address,additional_address FROM relay_bindings WHERE inbound_id=? AND tunnel_address<>''", inboundID, inboundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assigned := []netip.Prefix{}
	seen := map[netip.Addr]bool{}
	for rows.Next() {
		var primary, additional string
		if err := rows.Scan(&primary, &additional); err != nil {
			return nil, err
		}
		if primary == "" {
			return nil, errors.New("persisted WireGuard primary address missing")
		}
		families := map[bool]bool{}
		for _, value := range wireguardAddressPair(primary, additional) {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || prefix.Addr().Is4In6() || prefix.Bits() != prefix.Addr().BitLen() || seen[prefix.Addr()] || families[prefix.Addr().Is4()] {
				return nil, errors.New("invalid or conflicting persisted WireGuard address")
			}
			seen[prefix.Addr()] = true
			families[prefix.Addr().Is4()] = true
			assigned = append(assigned, prefix)
		}
	}
	return assigned, rows.Err()
}
func (r *Repository) DeviceAddress(ctx context.Context, inboundID, deviceID string, settings desiredstate.ProtocolSettings) (string, error) {
	addresses, err := r.DeviceAddresses(ctx, inboundID, deviceID, settings)
	if err != nil {
		return "", err
	}
	return addresses[0], nil
}

func wireguardAddressPair(primary, additional string) []string {
	if primary == "" {
		return nil
	}
	if additional == "" {
		return []string{primary}
	}
	return []string{primary, additional}
}

func (r *Repository) DeviceAddresses(ctx context.Context, inboundID, deviceID string, settings desiredstate.ProtocolSettings) ([]string, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var primary, additional string
	err = tx.QueryRowContext(ctx, "SELECT address,additional_address FROM wireguard_addresses WHERE inbound_id=? AND device_id=?", inboundID, deviceID).Scan(&primary, &additional)
	create := errors.Is(err, sql.ErrNoRows)
	if err != nil && !create {
		return nil, err
	}
	if !create && primary == "" {
		return nil, errors.New("persisted WireGuard device address missing")
	}
	addresses, err := ensureWireguardAddresses(ctx, tx, inboundID, settings, wireguardAddressPair(primary, additional))
	if err != nil {
		return nil, err
	}
	newAdditional := ""
	if len(addresses) > 1 {
		newAdditional = addresses[1]
	}
	if !create && primary == addresses[0] && additional == newAdditional {
		return addresses, nil
	}
	primary = addresses[0]
	additional = newAdditional
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO wireguard_addresses(inbound_id,device_id,address,additional_address) VALUES(?,?,?,?)", inboundID, deviceID, primary, additional)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE wireguard_addresses SET additional_address=? WHERE inbound_id=? AND device_id=?", additional, inboundID, deviceID)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return addresses, nil
}
