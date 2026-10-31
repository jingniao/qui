package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"time"
)

type Repository struct {
	Database *sql.DB
	Vault    *vault.Vault
}

func timestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }
func (r *Repository) encode(settings desiredstate.ProtocolSettings) ([]byte, error) {
	data, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	return r.Vault.Seal(data)
}
func (r *Repository) decode(data []byte) (desiredstate.ProtocolSettings, error) {
	var settings desiredstate.ProtocolSettings
	plaintext, err := r.Vault.Open(data)
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(plaintext, &settings)
	return settings, err
}
func mask(settings desiredstate.ProtocolSettings) desiredstate.ProtocolSettings {
	settings.PrivateKeyPEM = ""
	if settings.WireGuard != nil {
		copy := *settings.WireGuard
		copy.SecretKey = ""
		settings.WireGuard = &copy
	}
	return settings
}
func (r *Repository) Inbound(ctx context.Context, inboundID string, reveal bool) (Inbound, error) {
	var inbound Inbound
	var encoded []byte
	var users string
	err := r.Database.QueryRowContext(ctx, "SELECT id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,relay_credential_id,created_at,core FROM inbounds WHERE id=?", inboundID).Scan(&inbound.ID, &inbound.Name, &inbound.NodeID, &inbound.Protocol, &inbound.Listen, &inbound.Port, &inbound.Enabled, &inbound.SubscriptionVisible, &inbound.InternalRelay, &inbound.DefaultOutbound, &users, &encoded, &inbound.RelayCredentialID, &inbound.CreatedAt, &inbound.Core)
	if errors.Is(err, sql.ErrNoRows) {
		return inbound, ErrNotFound
	}
	if err != nil {
		return inbound, err
	}
	if err := json.Unmarshal([]byte(users), &inbound.AllowedUserIDs); err != nil {
		return inbound, err
	}
	inbound.Settings, err = r.decode(encoded)
	if err != nil {
		return inbound, err
	}
	var caps []byte
	if err := r.Database.QueryRowContext(ctx, "SELECT capabilities_json FROM nodes WHERE id=?", inbound.NodeID).Scan(&caps); err != nil {
		return inbound, err
	}
	c := &pb.Capabilities{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(caps, c); err != nil {
		return inbound, err
	}
	if core := node.SelectedCore(c, inbound.Core); core != nil {
		supported := core.Name == "xray" || core.SingboxUserStats
		gate := core.DeviceGateSupported && inbound.Protocol != "wireguard" && inbound.Protocol != "forward"
		inbound.UserTrafficSupported = &supported
		inbound.DeviceGateSupported = &gate
		coverage := node.DeviceGateCoverage(core, inbound.Protocol)
		inbound.DeviceGateCoverage = &coverage
	}
	inbound.HasTLSKey = inbound.Settings.PrivateKeyPEM != ""
	inbound.HasWireGuardKey = inbound.Settings.WireGuard != nil && inbound.Settings.WireGuard.SecretKey != ""
	if !reveal {
		inbound.Settings = mask(inbound.Settings)
	}
	return inbound, nil
}

// InboundAtListener returns the inbound already bound to one of a node's
// protocol listeners. A caller that must not create a second listener on the
// same port reads it first and reuses it instead.
func (r *Repository) InboundAtListener(ctx context.Context, nodeID, protocol, listen string, port int) (Inbound, error) {
	var inboundID string
	err := r.Database.QueryRowContext(ctx, "SELECT id FROM inbounds WHERE node_id=? AND protocol=? AND listen=? AND port=? ORDER BY created_at LIMIT 1", nodeID, protocol, listen, port).Scan(&inboundID)
	if errors.Is(err, sql.ErrNoRows) {
		return Inbound{}, ErrNotFound
	}
	if err != nil {
		return Inbound{}, err
	}
	return r.Inbound(ctx, inboundID, false)
}
func (r *Repository) Outbound(ctx context.Context, outboundID string, reveal bool) (Outbound, error) {
	var outbound Outbound
	var encoded []byte
	err := r.Database.QueryRowContext(ctx, "SELECT id,name,node_id,protocol,address,port,enabled,coalesce(target_node_id,''),coalesce(target_inbound_id,''),credential_id,encrypted_settings,support_tcp,support_udp,CASE WHEN failure_policy_v2='' THEN failure_policy ELSE failure_policy_v2 END,backup_outbound_id,created_at,probe_url,probe_interval FROM outbounds WHERE id=?", outboundID).Scan(&outbound.ID, &outbound.Name, &outbound.NodeID, &outbound.Protocol, &outbound.Address, &outbound.Port, &outbound.Enabled, &outbound.TargetNodeID, &outbound.TargetInboundID, &outbound.CredentialID, &encoded, &outbound.SupportTCP, &outbound.SupportUDP, &outbound.FailurePolicy, &outbound.BackupOutboundID, &outbound.CreatedAt, &outbound.ProbeURL, &outbound.ProbeInterval)
	if errors.Is(err, sql.ErrNoRows) {
		return outbound, ErrNotFound
	}
	if err != nil {
		return outbound, err
	}
	outbound.Settings, err = r.decode(encoded)
	if err != nil {
		return outbound, err
	}
	outbound.HasCredential = outbound.CredentialID != ""
	outbound.HasWireGuardKey = outbound.Settings.WireGuard != nil && outbound.Settings.WireGuard.SecretKey != ""
	if outbound.HasWireGuardKey {
		outbound.WireGuardPublicKey, err = publicKey(outbound.Settings.WireGuard.SecretKey)
		if err != nil {
			return outbound, err
		}
	}
	if !reveal {
		outbound.Settings = mask(outbound.Settings)
	}
	outbound.Health, err = r.Health(ctx, outbound.ID)
	if err != nil {
		return outbound, err
	}
	return outbound, nil
}
func (r *Repository) ids(ctx context.Context, table, nodeID string, page, size int) ([]string, int, error) {
	if table != "inbounds" && table != "outbounds" {
		return nil, 0, errors.New("invalid proxy table")
	}
	where := ""
	args := []any{}
	if nodeID != "" {
		where = " WHERE node_id=?"
		args = append(args, nodeID)
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM "+table+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM "+table+where+" ORDER BY created_at,id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	return ids, count, rows.Err()
}
func (r *Repository) Inbounds(ctx context.Context, nodeID string, page, size int, reveal bool) ([]Inbound, int, error) {
	ids, total, err := r.ids(ctx, "inbounds", nodeID, page, size)
	if err != nil {
		return nil, 0, err
	}
	items := []Inbound{}
	for _, id := range ids {
		inbound, err := r.Inbound(ctx, id, reveal)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, inbound)
	}
	return items, total, nil
}
func (r *Repository) Outbounds(ctx context.Context, nodeID string, page, size int, reveal bool) ([]Outbound, int, error) {
	ids, total, err := r.ids(ctx, "outbounds", nodeID, page, size)
	if err != nil {
		return nil, 0, err
	}
	items := []Outbound{}
	for _, id := range ids {
		outbound, err := r.Outbound(ctx, id, reveal)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, outbound)
	}
	return items, total, nil
}
func (r *Repository) SaveInbound(ctx context.Context, inboundID string, input InboundInput) (Inbound, error) {
	if err := ValidateInbound(input); err != nil {
		return Inbound{}, err
	}
	create := inboundID == ""
	relayCredential := ""
	created := timestamp()
	if create {
		var err error
		inboundID, err = id.New()
		if err != nil {
			return Inbound{}, err
		}
	} else {
		old, err := r.Inbound(ctx, inboundID, true)
		if err != nil {
			return Inbound{}, err
		}
		if old.NodeID != input.NodeID || old.Protocol != input.Protocol || old.InternalRelay != input.InternalRelay {
			return Inbound{}, errors.New("inbound node, protocol and relay role are immutable")
		}
		relayCredential = old.RelayCredentialID
		created = old.CreatedAt
	}
	encoded, err := r.encode(input.Settings)
	if err != nil {
		return Inbound{}, err
	}
	users, _ := json.Marshal(input.AllowedUserIDs)
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Inbound{}, err
	}
	defer tx.Rollback()
	if err := r.validateInboundPorts(ctx, tx, inboundID, input); err != nil {
		return Inbound{}, err
	}
	if input.Protocol == "wireguard" {
		pools, reserved, err := wireguardPools(input.Settings)
		if err != nil {
			return Inbound{}, err
		}
		assigned, err := assignedWireguardAddresses(ctx, tx, inboundID)
		if err != nil {
			return Inbound{}, err
		}
		if err := validateWireguardAssignments(pools, reserved, assigned); err != nil {
			return Inbound{}, err
		}
	}
	if create && input.InternalRelay {
		credential, err := r.createCredential(ctx, tx, input.Protocol, "", "")
		if err != nil {
			return Inbound{}, err
		}
		relayCredential = credential.ID
	}
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,relay_credential_id,created_at,core) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", inboundID, input.Name, input.NodeID, input.Protocol, input.Listen, input.Port, input.Enabled, input.SubscriptionVisible, input.InternalRelay, input.DefaultOutbound, string(users), encoded, relayCredential, created, input.Core)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE inbounds SET name=?,listen=?,port=?,enabled=?,subscription_visible=?,default_outbound=?,allowed_users_json=?,encrypted_settings=?,core=? WHERE id=?", input.Name, input.Listen, input.Port, input.Enabled, input.SubscriptionVisible, input.DefaultOutbound, string(users), encoded, input.Core, inboundID)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return Inbound{}, errors.New("node inbound port already in use")
		}
		return Inbound{}, err
	}
	if input.Protocol == "wireguard" && input.InternalRelay {
		if err := r.syncWireguardRelayDrafts(ctx, tx, inboundID, input); err != nil {
			return Inbound{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Inbound{}, err
	}
	return r.Inbound(ctx, inboundID, false)
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func (r *Repository) SaveOutbound(ctx context.Context, outboundID string, input OutboundInput) (Outbound, error) {
	if err := ValidateOutbound(input); err != nil {
		return Outbound{}, err
	}
	create := outboundID == ""
	credentialID := ""
	created := timestamp()
	if create {
		var err error
		outboundID, err = id.New()
		if err != nil {
			return Outbound{}, err
		}
	} else {
		old, err := r.Outbound(ctx, outboundID, true)
		if err != nil {
			return Outbound{}, err
		}
		if old.NodeID != input.NodeID || old.Protocol != input.Protocol || old.TargetInboundID != input.TargetInboundID {
			return Outbound{}, errors.New("outbound node, protocol and relay target are immutable")
		}
		credentialID = old.CredentialID
		created = old.CreatedAt
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Outbound{}, err
	}
	defer tx.Rollback()
	tunnelAddress := ""
	additionalAddress := ""
	if create && input.TargetInboundID != "" {
		credential, err := r.createCredential(ctx, tx, input.Protocol, "", "")
		if err != nil {
			return Outbound{}, err
		}
		credentialID = credential.ID
		if input.Protocol == "wireguard" {
			input.Settings.WireGuard.SecretKey = credential.Secret
		}
	}
	if input.TargetInboundID != "" && input.Protocol == "wireguard" {
		// Allocate from the target's original interface pool, never the
		// source outbound's peer routes (which intentionally include /0).
		var encrypted []byte
		if err := tx.QueryRowContext(ctx, "SELECT encrypted_settings FROM inbounds WHERE id=? AND protocol='wireguard' AND enabled=1 AND internal_relay=1", input.TargetInboundID).Scan(&encrypted); err != nil {
			return Outbound{}, err
		}
		targetSettings, err := r.decode(encrypted)
		if err != nil {
			return Outbound{}, err
		}
		var existing []string
		if !create {
			if err := tx.QueryRowContext(ctx, "SELECT tunnel_address,additional_address FROM relay_bindings WHERE outbound_id=? AND inbound_id=?", outboundID, input.TargetInboundID).Scan(&tunnelAddress, &additionalAddress); err != nil {
				return Outbound{}, err
			}
			if tunnelAddress == "" {
				return Outbound{}, errors.New("WireGuard relay address missing")
			}
			existing = wireguardAddressPair(tunnelAddress, additionalAddress)
		}
		addresses, err := ensureWireguardAddresses(ctx, tx, input.TargetInboundID, targetSettings, existing)
		if err != nil {
			return Outbound{}, err
		}
		tunnelAddress = addresses[0]
		if len(addresses) > 1 {
			additionalAddress = addresses[1]
		}
		input.Settings.WireGuard.Addresses = addresses
	}
	if input.Secret != "" {
		credential, err := r.createCredential(ctx, tx, input.Protocol, input.Username, input.Secret)
		if err != nil {
			return Outbound{}, err
		}
		credentialID = credential.ID
	}
	encoded, err := r.encode(input.Settings)
	if err != nil {
		return Outbound{}, err
	}
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,target_node_id,target_inbound_id,credential_id,encrypted_settings,support_tcp,support_udp,failure_policy,backup_outbound_id,created_at,probe_url,probe_interval) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", outboundID, input.Name, input.NodeID, input.Protocol, input.Address, input.Port, input.Enabled, nullable(input.TargetNodeID), nullable(input.TargetInboundID), credentialID, encoded, input.SupportTCP, input.SupportUDP, legacyFailure(input.FailurePolicy, false), input.BackupOutboundID, created, input.ProbeURL, input.ProbeInterval)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE outbounds SET name=?,address=?,port=?,enabled=?,credential_id=?,encrypted_settings=?,support_tcp=?,support_udp=?,failure_policy=?,backup_outbound_id=?,probe_url=?,probe_interval=? WHERE id=?", input.Name, input.Address, input.Port, input.Enabled, credentialID, encoded, input.SupportTCP, input.SupportUDP, legacyFailure(input.FailurePolicy, false), input.BackupOutboundID, input.ProbeURL, input.ProbeInterval, outboundID)
	}
	if err != nil {
		return Outbound{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE outbounds SET failure_policy_v2=? WHERE id=?", input.FailurePolicy, outboundID); err != nil {
		return Outbound{}, err
	}
	if create && input.TargetInboundID != "" {
		if _, err := tx.ExecContext(ctx, "INSERT INTO relay_bindings(outbound_id,inbound_id,credential_id,tunnel_address,additional_address) VALUES(?,?,?,?,?)", outboundID, input.TargetInboundID, credentialID, tunnelAddress, additionalAddress); err != nil {
			return Outbound{}, err
		}
	} else if input.TargetInboundID != "" && input.Protocol == "wireguard" {
		if _, err := tx.ExecContext(ctx, "UPDATE relay_bindings SET additional_address=? WHERE outbound_id=?", additionalAddress, outboundID); err != nil {
			return Outbound{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Outbound{}, err
	}
	return r.Outbound(ctx, outboundID, false)
}
func (r *Repository) createCredential(ctx context.Context, tx *sql.Tx, protocol, username, secret string) (desiredstate.Credential, error) {
	credentialID, err := id.New()
	if err != nil {
		return desiredstate.Credential{}, err
	}
	if secret == "" {
		generated, err := user.GenerateCredential(protocol)
		if err != nil {
			return desiredstate.Credential{}, err
		}
		secret = generated.Value
		if username == "" {
			username = generated.Username
		}
	}
	if protocol == "tuic" {
		if err := desiredstate.ValidateUUID(username); err != nil {
			return desiredstate.Credential{}, err
		}
	}
	if (protocol == "http" || protocol == "socks5") && username == "" {
		username = "relay-" + credentialID[:12]
	}
	data, err := r.Vault.Seal([]byte(secret))
	if err != nil {
		return desiredstate.Credential{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO relay_credentials(id,protocol,username,encrypted_secret,created_at) VALUES(?,?,?,?,?)", credentialID, protocol, username, data, timestamp()); err != nil {
		return desiredstate.Credential{}, err
	}
	return desiredstate.Credential{ID: credentialID, Protocol: protocol, Username: username, Secret: secret, Internal: true}, nil
}
func (r *Repository) Credential(ctx context.Context, credentialID string) (desiredstate.Credential, error) {
	credential := desiredstate.Credential{Internal: true}
	var sealed []byte
	err := r.Database.QueryRowContext(ctx, "SELECT id,protocol,username,encrypted_secret FROM relay_credentials WHERE id=?", credentialID).Scan(&credential.ID, &credential.Protocol, &credential.Username, &sealed)
	if err != nil {
		return credential, err
	}
	secret, err := r.Vault.Open(sealed)
	credential.Secret = string(secret)
	return credential, err
}

type Binding struct {
	OutboundID, CredentialID, TunnelAddress string
	TunnelAddresses                         []string
}

func (r *Repository) Bindings(ctx context.Context, inboundID string) ([]Binding, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT outbound_id,credential_id,tunnel_address,additional_address FROM relay_bindings WHERE inbound_id=? ORDER BY outbound_id", inboundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Binding{}
	for rows.Next() {
		var item Binding
		var additional string
		if err := rows.Scan(&item.OutboundID, &item.CredentialID, &item.TunnelAddress, &additional); err != nil {
			return nil, err
		}
		item.TunnelAddresses = wireguardAddressPair(item.TunnelAddress, additional)
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) DeleteInbound(ctx context.Context, id string) error {
	if _, err := r.Inbound(ctx, id, false); err != nil {
		return err
	}
	_, err := r.Database.ExecContext(ctx, "DELETE FROM inbounds WHERE id=?", id)
	if err != nil {
		return errors.New("inbound is referenced by a relay outbound")
	}
	return nil
}
func (r *Repository) DeleteOutbound(ctx context.Context, id string) error {
	if _, err := r.Outbound(ctx, id, false); err != nil {
		return err
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM inbounds WHERE default_outbound=?) +
 (SELECT count(*) FROM outbound_groups WHERE backup_outbound_id=?) +
	 (SELECT count(*) FROM outbounds WHERE backup_outbound_id=?) +
	 (SELECT count(*) FROM proxy_chains WHERE final_outbound_id=? OR backup_target_id=?)`, id, id, id, id, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("outbound is referenced by an inbound, group, chain or backup")
	}
	_, err := r.Database.ExecContext(ctx, "DELETE FROM outbounds WHERE id=?", id)
	return err
}
