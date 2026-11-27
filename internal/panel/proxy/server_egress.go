package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/id"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

const hostEgressOutbound = "_server_egress_direct"
const serverEgressCipher = "chacha20-ietf-poly1305"
const serverEgressOutboundPrefix = "_server_egress_to_"

var ErrServerEgressListenerInUse = errors.New("服务器出站监听仍被引用")

type serverEgressPolicies interface {
	ServerEgressTargets(context.Context, string, string) ([]node.Node, error)
}

func serverEgressAction(kind string) bool {
	return kind == "ServerEgress" || kind == "ServerEgressGroup"
}

func requireServerEgressAgent(server node.Node) error {
	const minimum = "1.12.4"
	version, _, _ := strings.Cut(strings.TrimPrefix(server.AgentVersion, "v"), "-rc.")
	parts := strings.Split(version, ".")
	if len(parts) == 3 {
		var numbers [3]uint64
		var err error
		for i, part := range parts {
			numbers[i], err = strconv.ParseUint(part, 10, 64)
			if err != nil {
				break
			}
		}
		if err == nil && slices.Compare(numbers[:], []uint64{1, 12, 4}) >= 0 {
			return nil
		}
	}
	return fmt.Errorf("服务器「%s」需要升级到 Agent %s 或更新版本才能发布服务器出站", server.Name, minimum)
}

func enableHostEgress(state *desiredstate.State) {
	if !slices.ContainsFunc(state.Outbounds, func(out desiredstate.Outbound) bool { return out.ID == hostEgressOutbound }) {
		state.Outbounds = append(state.Outbounds, desiredstate.Outbound{
			ID: hostEgressOutbound, Protocol: "direct", Enabled: true, UseHostNetwork: true, SupportTCP: true, SupportUDP: true,
		})
	}
}

func (r *Repository) serverEgressInboundID(ctx context.Context, nodeID string) (string, error) {
	var inboundID string
	err := r.Database.QueryRowContext(ctx, "SELECT inbound_id FROM server_egress_relays WHERE node_id=?", nodeID).Scan(&inboundID)
	return inboundID, err
}

func (r *Repository) ensureServerEgressInbound(ctx context.Context, target node.Node) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	address, err := netip.ParseAddr(target.PublicIP)
	if err != nil {
		return fmt.Errorf("服务器 %s 没有可用的公网 IP", target.Name)
	}
	listen := "0.0.0.0"
	if address.Unmap().Is6() {
		listen = "::"
	}
	if err := ValidateInboundCore(target.Capabilities, "", "shadowsocks"); err != nil {
		return err
	}
	var inboundID, currentListen string
	err = tx.QueryRowContext(ctx, "SELECT i.id,i.listen FROM server_egress_relays r JOIN inbounds i ON i.id=r.inbound_id WHERE r.node_id=?", target.ID).Scan(&inboundID, &currentListen)
	if err == nil {
		if currentListen != listen {
			if err := r.RequireUnusedServerEgressListener(ctx, tx, target.ID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE inbounds SET listen=? WHERE id=?", listen, inboundID); err != nil {
				return err
			}
			return r.commitDraft(tx)
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	occupied, err := r.portOccupants(ctx, tx, target.ID, nil)
	if err != nil {
		return err
	}
	ports, err := allocatePorts(ctx, tx, target.ID, 1, occupied, nil)
	if err != nil {
		return err
	}
	inboundID, err = id.New()
	if err != nil {
		return err
	}
	credential, err := r.createCredential(ctx, tx, "shadowsocks", "", "")
	if err != nil {
		return err
	}
	settings, err := r.encode(desiredstate.ProtocolSettings{Method: serverEgressCipher})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,relay_credential_id,created_at)
		VALUES(?,'[节点策略] 服务器出站',?,'shadowsocks',?,?,1,0,1,'direct','[]',?,?,?)`,
		inboundID, target.ID, listen, ports[0], settings, credential.ID, timestamp()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO server_egress_relays(node_id,inbound_id) VALUES(?,?)", target.ID, inboundID); err != nil {
		return err
	}
	return r.commitDraft(tx)
}

func (s *Service) serverEgressListener(ctx context.Context, nodeID string) (Inbound, desiredstate.Credential, error) {
	inboundID, err := s.Repository.serverEgressInboundID(ctx, nodeID)
	if err != nil {
		return Inbound{}, desiredstate.Credential{}, err
	}
	inbound, err := s.Repository.Inbound(ctx, inboundID, true)
	if err != nil {
		return inbound, desiredstate.Credential{}, err
	}
	credential, err := s.Repository.Credential(ctx, inbound.RelayCredentialID)
	return inbound, credential, err
}

func (s *Service) addServerEgressListener(ctx context.Context, target node.Node, state *desiredstate.State, intent []string) ([]string, error) {
	record, credential, err := s.serverEgressListener(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	inbound, err := renderInboundBase(target, record)
	if err != nil {
		return nil, err
	}
	enableHostEgress(state)
	inbound.InternalRelay = true
	inbound.DefaultOutbound = hostEgressOutbound
	inbound.CredentialIDs = []string{credential.ID}
	if !slices.ContainsFunc(state.Credentials, func(c desiredstate.Credential) bool { return c.ID == credential.ID }) {
		state.Credentials = append(state.Credentials, credential)
	}
	index := slices.IndexFunc(state.Inbounds, func(entry desiredstate.Inbound) bool { return entry.ID == inbound.ID })
	if index < 0 {
		state.Inbounds = append(state.Inbounds, inbound)
	} else {
		state.Inbounds[index] = inbound
	}
	if intent != nil && !slices.Contains(intent, inbound.ID) {
		intent = append(intent, inbound.ID)
	}
	return intent, nil
}

func (s *Service) expandServerEgress(ctx context.Context, source node.Node, state *desiredstate.State) error {
	if _, err := s.Repository.serverEgressInboundID(ctx, source.ID); err == nil {
		if _, err := s.addServerEgressListener(ctx, source, state, nil); err != nil {
			return err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	for i := range state.Policies {
		rule := &state.Policies[i]
		if !serverEgressAction(rule.Action.Type) {
			continue
		}
		if !rule.Enabled {
			rule.Action = desiredstate.Action{Type: "Block"}
			continue
		}
		policies, ok := s.Policies.(serverEgressPolicies)
		if !ok {
			return errors.New("server egress policy resolver unavailable")
		}
		targets, err := policies.ServerEgressTargets(ctx, rule.Action.Type, rule.Action.TargetID)
		if err != nil {
			return fmt.Errorf("策略 %s：%w", rule.Name, err)
		}
		members := make([]desiredstate.GroupMember, 0, len(targets))
		for _, target := range targets {
			outboundID, err := s.addServerEgressOutbound(ctx, source, target, state, rule.Match.Network)
			if err != nil {
				return fmt.Errorf("策略 %s 的目标 %s：%w", rule.Name, target.Name, err)
			}
			members = append(members, desiredstate.GroupMember{OutboundID: outboundID, Priority: len(members) + 1, Weight: 1})
		}
		if rule.Action.Type == "ServerEgress" {
			rule.Action = desiredstate.Action{Type: "Outbound", TargetID: members[0].OutboundID, Failover: "Block"}
		} else {
			groupID := "_server_egress_group_" + rule.ID
			state.OutboundGroups = append(state.OutboundGroups, desiredstate.OutboundGroup{
				ID: groupID, Strategy: "primary", Members: members, Failover: "Block",
				ProbeURL: "https://www.gstatic.com/generate_204", ProbeInterval: 30,
			})
			rule.Action = desiredstate.Action{Type: "OutboundGroup", TargetID: groupID, Failover: "Block"}
		}
	}
	return nil
}

func (s *Service) addServerEgressOutbound(ctx context.Context, source, target node.Node, state *desiredstate.State, network string) (string, error) {
	if source.ID == target.ID {
		enableHostEgress(state)
		return hostEgressOutbound, nil
	}
	targetTCP, targetUDP := serverEgressTransport(node.SelectedCore(target.Capabilities, ""), true)
	sourceTCP, sourceUDP := true, true
	primary := source.Capabilities.GetCoreName()
	names := map[string]bool{primary: true}
	for _, inbound := range state.Inbounds {
		if desiredstate.CarriesUsers(inbound.Core) {
			names[inbound.Core] = true
		}
	}
	for name := range names {
		tcp, udp := serverEgressTransport(node.SelectedCore(source.Capabilities, name), false)
		sourceTCP, sourceUDP = sourceTCP && tcp, sourceUDP && udp
	}
	tcp, udp := sourceTCP && targetTCP, sourceUDP && targetUDP
	if network != "udp" && !tcp || network != "tcp" && !udp {
		return "", errors.New("服务器间中继不支持策略要求的 TCP/UDP")
	}
	outboundID := serverEgressOutboundPrefix + target.ID
	if slices.ContainsFunc(state.Outbounds, func(out desiredstate.Outbound) bool { return out.ID == outboundID }) {
		return outboundID, nil
	}
	inbound, credential, err := s.serverEgressListener(ctx, target.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("请先从节点策略页发布，以创建目标服务器的受管监听")
	}
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(state.Credentials, func(c desiredstate.Credential) bool { return c.ID == credential.ID }) {
		state.Credentials = append(state.Credentials, credential)
	}
	state.Outbounds = append(state.Outbounds, desiredstate.Outbound{
		ID: outboundID, Protocol: inbound.Protocol, Enabled: true, Address: target.PublicIP, Port: inbound.Port,
		CredentialID: credential.ID, TargetNodeID: target.ID, Settings: inbound.Settings,
		SupportTCP:    tcp,
		SupportUDP:    udp,
		FailurePolicy: "Block",
	})
	return outboundID, nil
}

func serverEgressTransport(core *pb.CoreCapability, inbound bool) (bool, bool) {
	for _, protocol := range core.GetProtocols() {
		if protocol.GetProtocol() == "shadowsocks" && (inbound && protocol.GetInbound() || !inbound && protocol.GetOutbound()) {
			return protocol.GetTcp(), protocol.GetUdp()
		}
	}
	return false, false
}

func (r *Repository) publishedServerEgressReferences(ctx context.Context, targetID string) ([]node.Blocker, error) {
	if _, err := r.serverEgressInboundID(ctx, targetID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return r.serverEgressReferences(ctx, r.Database, targetID)
}

type serverEgressQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r *Repository) RequireUnusedServerEgressListener(ctx context.Context, q serverEgressQuery, targetID string) error {
	refs, err := r.serverEgressReferences(ctx, q, targetID)
	if err != nil {
		return err
	}
	if len(refs) != 0 {
		return fmt.Errorf("%w：服务器「%s」有运行中或待确认的配置", ErrServerEgressListenerInUse, refs[0].Name)
	}
	return nil
}

func (r *Repository) serverEgressReferences(ctx context.Context, q serverEgressQuery, targetID string) ([]node.Blocker, error) {
	rows, err := q.QueryContext(ctx, `WITH live AS (
		SELECT node_id,revision FROM node_runtime_state
		UNION SELECT node_id,revision FROM deployment_targets WHERE status='pending'
		UNION SELECT node_id,revision FROM deployment_commands WHERE status='pending'
	)
	SELECT n.id,n.name,c.encrypted_state
	FROM live s JOIN nodes n ON n.id=s.node_id JOIN config_revisions c ON c.id=s.revision
	WHERE n.id<>? AND n.deleted_at IS NULL ORDER BY n.id,c.id`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []node.Blocker
	seen := map[string]bool{}
	for rows.Next() {
		ref := node.Blocker{Kind: "server_egress_published"}
		var sealed []byte
		if err := rows.Scan(&ref.ID, &ref.Name, &sealed); err != nil {
			return nil, err
		}
		data, err := r.Vault.Open(sealed)
		if err != nil {
			return nil, err
		}
		state, err := desiredstate.Decode(data)
		clear(data)
		if err != nil {
			return nil, err
		}
		if !seen[ref.ID] && slices.ContainsFunc(state.ReachableOutbounds(), func(out desiredstate.Outbound) bool {
			return out.ID == serverEgressOutboundPrefix+targetID && out.Enabled
		}) {
			seen[ref.ID] = true
			refs = append(refs, ref)
		}
	}
	return refs, rows.Err()
}

// ServerEgressSnapshots prepares the paired listeners before constructing the
// entry revision. Target-only nodes carry their published settings forward.
func (s *Service) ServerEgressSnapshots(ctx context.Context, entryID string, users AccessReader,
	published func(context.Context, string) (desiredstate.State, []string, error),
) ([]NodeSnapshot, error) {
	policies, ok := s.Policies.(serverEgressPolicies)
	if !ok {
		return nil, nil
	}
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	rules, err := s.Policies.ForNode(ctx, entryID)
	if err != nil {
		return nil, err
	}
	hasServerEgress := false
	targets := []node.Node{}
	seen := map[string]bool{entryID: true}
	for _, rule := range rules {
		if !rule.Enabled || !serverEgressAction(rule.Action.Type) {
			continue
		}
		hasServerEgress = true
		nodes, err := policies.ServerEgressTargets(ctx, rule.Action.Type, rule.Action.TargetID)
		if err != nil {
			return nil, err
		}
		for _, target := range nodes {
			if !seen[target.ID] {
				seen[target.ID] = true
				targets = append(targets, target)
			}
		}
	}
	if !hasServerEgress {
		return nil, nil
	}
	entry, err := s.Nodes.Get(ctx, entryID)
	if err != nil {
		return nil, err
	}
	if err := requireServerEgressAgent(entry); err != nil {
		return nil, err
	}
	for _, target := range targets {
		if err := requireServerEgressAgent(target); err != nil {
			return nil, err
		}
	}
	for _, target := range targets {
		if err := s.Repository.ensureServerEgressInbound(ctx, target); err != nil {
			return nil, fmt.Errorf("准备出站服务器 %s：%w", target.Name, err)
		}
	}
	snapshots := make([]NodeSnapshot, 0, len(targets)+1)
	for _, target := range targets {
		state, intent, err := published(ctx, target.ID)
		expected := state.Revision
		if errors.Is(err, sql.ErrNoRows) {
			// A target-only first deployment installs just the listener.
			// Its drafts may contain the same global policy, which would
			// otherwise add outbound dependencies back to the other targets.
			state = desiredstate.Baseline(target.ID, desiredstate.NetworkOrNil(
				target.OutboundSourceIP, target.OutboundInterface, target.HostSourceIPv4, target.HostSourceIPv6,
			))
			intent, err = []string{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("读取出站服务器 %s 的配置：%w", target.Name, err)
		}
		intent, err = s.addServerEgressListener(ctx, target, &state, intent)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, NodeSnapshot{State: state, InboundIntent: intent, ExpandedOnly: true, ExpectedPreviousRevision: &expected})
	}
	state, intent, err := s.SnapshotIntent(ctx, entryID, users)
	if err != nil {
		return nil, err
	}
	return append(snapshots, NodeSnapshot{State: state, InboundIntent: intent}), nil
}
