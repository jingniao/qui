package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/pkg/contracts/desiredstate"
	"sort"
	"strconv"
)

var ErrDiffNode = errors.New("revisions belong to different nodes")

type Change struct {
	Category string `json:"category"`
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Field    string `json:"field"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Hidden   bool   `json:"hidden"`
}

type RevisionDiff struct {
	NodeID       string   `json:"nodeId"`
	BaseRevision int64    `json:"baseRevision"`
	Revision     int64    `json:"revision"`
	Changes      []Change `json:"changes"`
}

// Diff reads both immutable states and the optional running base in one read
// transaction. It never sends a snapshot, secret value, or secret digest to UI.
// nil base means the node's centrally confirmed running version; zero means
// empty configuration and does not require an archived Revision 0 snapshot.
func (r *Repository) Diff(ctx context.Context, revision int64, base *int64) (RevisionDiff, error) {
	result := RevisionDiff{Revision: revision, Changes: []Change{}}
	if revision < 1 || base != nil && *base < 0 {
		return result, errors.New("invalid comparison revision")
	}
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	read := func(id int64) (desiredstate.State, error) {
		var sealed []byte
		if err := tx.QueryRowContext(ctx, "SELECT encrypted_state FROM config_revisions WHERE id=?", id).Scan(&sealed); err != nil {
			return desiredstate.State{}, err
		}
		data, err := r.Vault.Open(sealed)
		if err != nil {
			return desiredstate.State{}, err
		}
		state, err := desiredstate.Decode(data)
		if err != nil {
			return desiredstate.State{}, err
		}
		if state.Revision != id {
			return desiredstate.State{}, errors.New("immutable revision identity mismatch")
		}
		return state, nil
	}
	next, err := read(revision)
	if err != nil {
		return result, err
	}
	result.NodeID = next.NodeID
	if base != nil {
		result.BaseRevision = *base
	} else {
		err := tx.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", next.NodeID).Scan(&result.BaseRevision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
	}
	previous := desiredstate.State{NodeID: next.NodeID}
	if result.BaseRevision > 0 {
		previous, err = read(result.BaseRevision)
		if err != nil {
			return result, err
		}
		if previous.NodeID != next.NodeID {
			return result, ErrDiffNode
		}
	}
	result.Changes = compareStates(previous, next)
	return result, tx.Commit()
}

type reviewValue struct {
	value  string
	hidden bool
}
type reviewRecord struct {
	category, id string
	fields       map[string]reviewValue
}

// Public fields are selected explicitly from validated types. Adding a new
// contract field does not expose it automatically through a comparison.
func publicFields(record *reviewRecord, prefix string, value any, names ...string) {
	data, _ := json.Marshal(value)
	var fields map[string]json.RawMessage
	json.Unmarshal(data, &fields)
	for _, name := range names {
		if raw, exists := fields[name]; exists {
			record.fields[prefix+name] = reviewValue{value: string(raw)}
		}
	}
}

func hiddenField(record *reviewRecord, name, value string) {
	if value != "" {
		// Raw comparison stays in memory; no hash or recoverable value is
		// included in the result. Equal hidden values create no change.
		record.fields[name] = reviewValue{value: value, hidden: true}
	}
}

func reviewSettings(record *reviewRecord, settings desiredstate.ProtocolSettings) {
	publicFields(record, "settings.", settings, "transport", "security", "server_name", "method", "flow", "path", "service_name")
	if settings.QUIC != nil {
		publicFields(record, "settings.quic.", settings.QUIC, "up_mbps", "down_mbps", "congestion_control", "udp_relay_mode")
	}
	hiddenField(record, "settings.certificate_pem", settings.CertificatePEM)
	hiddenField(record, "settings.private_key_pem", settings.PrivateKeyPEM)
	hiddenField(record, "settings.trusted_ca_pem", settings.TrustedCAPEM)
	if settings.WireGuard == nil {
		return
	}
	publicFields(record, "settings.wireguard.", settings.WireGuard, "addresses", "mtu")
	hiddenField(record, "settings.wireguard.secret_key", settings.WireGuard.SecretKey)
	for index, peer := range settings.WireGuard.Peers {
		prefix := "settings.wireguard.peers[" + strconv.Itoa(index) + "]."
		publicFields(record, prefix, peer, "endpoint", "allowed_ips", "keep_alive", "credential_id")
		hiddenField(record, prefix+"public_key", peer.PublicKey)
	}
}

func reviewState(state desiredstate.State) map[string]reviewRecord {
	records := map[string]reviewRecord{}
	add := func(category, id string) *reviewRecord {
		return &reviewRecord{category: category, id: id, fields: map[string]reviewValue{}}
	}
	save := func(record *reviewRecord) { records[record.category+"/"+record.id] = *record }
	for _, value := range state.Users {
		record := add("users", value.ID)
		publicFields(record, "", value, "enabled", "expires_at")
		save(record)
	}
	for _, value := range state.Credentials {
		record := add("credentials", value.ID)
		publicFields(record, "", value, "user_id", "device_id", "protocol", "internal")
		hiddenField(record, "username", value.Username)
		hiddenField(record, "secret", value.Secret)
		hiddenField(record, "public_key", value.PublicKey)
		save(record)
	}
	for _, value := range state.Inbounds {
		record := add("inbounds", value.ID)
		publicFields(record, "", value, "protocol", "listen", "port", "enabled", "credential_ids", "default_outbound", "subscription_visible")
		reviewSettings(record, value.Settings)
		save(record)
	}
	for _, value := range state.Outbounds {
		record := add("outbounds", value.ID)
		publicFields(record, "", value, "protocol", "address", "port", "enabled", "credential_id", "target_node_id", "failure_policy", "backup_target_id", "support_tcp", "support_udp", "probe_url", "probe_interval")
		reviewSettings(record, value.Settings)
		save(record)
	}
	for _, value := range state.OutboundGroups {
		record := add("outbound_groups", value.ID)
		publicFields(record, "", value, "strategy", "members", "probe_url", "probe_interval", "failover", "backup_target_id")
		save(record)
	}
	for _, value := range state.ProxyChains {
		record := add("proxy_chains", value.ID)
		publicFields(record, "", value, "node_ids", "outbound_ids", "failover", "final_outbound_id", "backup_target_id")
		save(record)
	}
	for _, value := range state.Policies {
		record := add("policies", value.ID)
		publicFields(record, "", value, "priority", "enabled", "scope", "match", "action", "name", "timezone", "logging")
		save(record)
	}
	// A node without settings carries no record here, so a revision that adds,
	// changes or clears them is still reported as a change.
	if state.Network != nil {
		record := add("network", "node")
		publicFields(record, "", *state.Network, "outbound_source_ip", "outbound_interface", "host_source_ipv4", "host_source_ipv6")
		save(record)
	}
	if state.Revision > 0 {
		record := add("logging", "logging")
		publicFields(record, "", state.Logging, "domain_only")
		save(record)
	}
	return records
}

func compareStates(previous, next desiredstate.State) []Change {
	before, after := reviewState(previous), reviewState(next)
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	ordered := []string{}
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := []Change{}
	for _, key := range ordered {
		old, had := before[key]
		current, has := after[key]
		kind := "modified"
		if !had {
			kind = "added"
			old = reviewRecord{category: current.category, id: current.id}
		}
		if !has {
			kind = "removed"
			current = reviewRecord{category: old.category, id: old.id}
		}
		fields := map[string]bool{}
		for field := range old.fields {
			fields[field] = true
		}
		for field := range current.fields {
			fields[field] = true
		}
		names := []string{}
		for name := range fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			left, leftExists := old.fields[name]
			right, rightExists := current.fields[name]
			if had && has && leftExists == rightExists && left == right {
				continue
			}
			change := Change{Category: current.category, ID: current.id, Kind: kind, Field: name, Hidden: left.hidden || right.hidden, Before: left.value, After: right.value}
			if change.Hidden {
				change.Before, change.After = "", ""
				if leftExists {
					change.Before = "[已隐藏]"
				}
				if rightExists {
					change.After = "[已隐藏]"
				}
			}
			result = append(result, change)
		}
	}
	return result
}
