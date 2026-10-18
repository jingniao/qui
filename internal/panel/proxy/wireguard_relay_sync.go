package proxy

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strconv"

	"proxycontrol/pkg/contracts/desiredstate"
)

// Synchronize draft records inside the inbound save transaction. Published
// revisions and node runtime state are changed only by Deployment.
func (r *Repository) syncWireguardRelayDrafts(ctx context.Context, tx *sql.Tx, inboundID string, input InboundInput) error {
	rows, err := tx.QueryContext(ctx, `SELECT b.outbound_id,b.credential_id,b.tunnel_address,b.additional_address,
 o.protocol,o.target_inbound_id,coalesce(o.target_node_id,''),o.credential_id,o.address,o.port,o.encrypted_settings,
 c.protocol,c.encrypted_secret
 FROM relay_bindings b JOIN outbounds o ON o.id=b.outbound_id
 JOIN relay_credentials c ON c.id=b.credential_id WHERE b.inbound_id=? ORDER BY b.outbound_id`, inboundID)
	if err != nil {
		return err
	}
	type draft struct {
		id, credential, primary, additional, protocol, targetInbound, targetNode, outboundCredential, address, credentialProtocol string
		port                                                                                                                      int
		settings, secret                                                                                                          []byte
	}
	var drafts []draft
	for rows.Next() {
		var d draft
		if err := rows.Scan(&d.id, &d.credential, &d.primary, &d.additional, &d.protocol, &d.targetInbound, &d.targetNode, &d.outboundCredential, &d.address, &d.port, &d.settings, &d.credentialProtocol, &d.secret); err != nil {
			rows.Close()
			return err
		}
		drafts = append(drafts, d)
	}
	readErr := rows.Err()
	if err := errors.Join(readErr, rows.Close()); err != nil {
		return err
	}
	var expected int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM relay_bindings WHERE inbound_id=?", inboundID).Scan(&expected); err != nil {
		return err
	}
	if expected != len(drafts) {
		return errors.New("WireGuard relay draft or credential missing")
	}
	if len(drafts) == 0 {
		return nil
	}
	serverKey, err := publicKey(input.Settings.WireGuard.SecretKey)
	if err != nil {
		return err
	}
	var targetAddress string
	if err := tx.QueryRowContext(ctx, "SELECT public_ip FROM nodes WHERE id=?", input.NodeID).Scan(&targetAddress); err != nil {
		return err
	}
	for _, d := range drafts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.protocol != "wireguard" || d.credentialProtocol != "wireguard" || d.targetInbound != inboundID || d.targetNode != input.NodeID || d.credential != d.outboundCredential || d.primary == "" {
			return errors.New("WireGuard relay binding conflicts with its source draft")
		}
		settings, err := r.decode(d.settings)
		if err != nil {
			return err
		}
		secret, err := r.Vault.Open(d.secret)
		if err != nil {
			return err
		}
		if settings.WireGuard == nil || len(settings.WireGuard.Peers) != 1 || !bytes.Equal(secret, []byte(settings.WireGuard.SecretKey)) {
			return errors.New("WireGuard relay draft identity requires reconciliation")
		}
		original, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		addresses, err := ensureWireguardAddresses(ctx, tx, inboundID, input.Settings, wireguardAddressPair(d.primary, d.additional))
		if err != nil {
			return err
		}
		address := targetAddress
		if address == "" {
			address = d.address
		}
		if address == "" {
			return errors.New("WireGuard relay target public endpoint missing")
		}
		settings.WireGuard.Addresses = addresses
		settings.WireGuard.MTU = input.Settings.WireGuard.MTU
		settings.WireGuard.Peers[0].PublicKey = serverKey
		settings.WireGuard.Peers[0].Endpoint = net.JoinHostPort(address, strconv.Itoa(input.Port))
		if err := desiredstate.ValidateSettings("wireguard", settings, false); err != nil {
			return err
		}
		updated, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		additional := ""
		if len(addresses) > 1 {
			additional = addresses[1]
		}
		if additional != d.additional {
			result, err := tx.ExecContext(ctx, "UPDATE relay_bindings SET additional_address=? WHERE outbound_id=? AND inbound_id=? AND tunnel_address=?", additional, d.id, inboundID, d.primary)
			if err != nil {
				return err
			}
			if count, err := result.RowsAffected(); err != nil || count != 1 {
				return errors.New("WireGuard relay binding changed during synchronization")
			}
		}
		if address == d.address && input.Port == d.port && bytes.Equal(original, updated) {
			continue
		}
		encoded, err := r.Vault.Seal(updated)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "UPDATE outbounds SET address=?,port=?,encrypted_settings=? WHERE id=? AND target_inbound_id=? AND credential_id=?", address, input.Port, encoded, d.id, inboundID, d.credential)
		if err != nil {
			return err
		}
		if count, err := result.RowsAffected(); err != nil || count != 1 {
			return errors.New("WireGuard source draft changed during synchronization")
		}
	}
	return nil
}
