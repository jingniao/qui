package deployment

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRevisionDiffReadsImmutableSnapshotsMasksSecretsAndUsesRunningBase(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	key, err := vault.Open(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{Database: database, Vault: key}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Diff node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certificates := testutil.Certificates(t)
	cert, err := os.ReadFile(certificates.Server.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	private, err := os.ReadFile(certificates.Server.Key)
	if err != nil {
		t.Fatal(err)
	}
	wgKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	peerKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("p", 32)))
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: []desiredstate.Credential{{ID: "credential", Protocol: "http", Username: "private-account-name", Secret: "private-first-password"}},
		Inbounds:    []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}, Settings: desiredstate.ProtocolSettings{Security: "tls", CertificatePEM: string(cert), PrivateKeyPEM: string(private)}}},
		Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "wg", Protocol: "wireguard", Address: "192.0.2.1", Port: 51820, Enabled: true, Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: wgKey, Addresses: []string{"10.0.0.1/32"}, Peers: []desiredstate.WireGuardPeer{{PublicKey: peerKey, AllowedIPs: []string{"0.0.0.0/0"}}}}}}},
	}
	first, err := repository.Create(ctx, state, "owner", "first")
	if err != nil {
		t.Fatal(err)
	}
	state.Inbounds[0].Port = 1081
	state.Credentials[0].Secret = "private-second-password"
	state.Outbounds[1].Settings.WireGuard.SecretKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32)))
	second, err := repository.Create(ctx, state, "owner", "second")
	if err != nil {
		t.Fatal(err)
	}
	// The confirmed base must follow a rollback too, rather than selecting the
	// latest successful historical revision or mutable business drafts.
	if _, err := database.Exec("INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?)", n.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	var writesBefore, writesAfter int64
	database.QueryRow("SELECT total_changes()").Scan(&writesBefore)
	diff, err := repository.Diff(ctx, second.ID, nil)
	if err != nil || diff.BaseRevision != first.ID || diff.NodeID != n.ID || len(diff.Changes) != 3 {
		t.Fatal("wrong immutable comparison", diff, err)
	}
	database.QueryRow("SELECT total_changes()").Scan(&writesAfter)
	if writesBefore != writesAfter {
		t.Fatal("comparison changed database or running configuration")
	}
	fields := map[string]Change{}
	for _, change := range diff.Changes {
		fields[change.Field] = change
	}
	if change := fields["port"]; change.Before != "1080" || change.After != "1081" || change.Hidden || change.Kind != "modified" {
		t.Fatal("public change missing", change)
	}
	for _, field := range []string{"secret", "settings.wireguard.secret_key"} {
		if change := fields[field]; !change.Hidden || change.Before != "[已隐藏]" || change.After != "[已隐藏]" {
			t.Fatal("secret change missing or exposed", field, change)
		}
	}
	zero := int64(0)
	added, err := repository.Diff(ctx, second.ID, &zero)
	if err != nil || len(added.Changes) < len(diff.Changes) {
		t.Fatal(added, err)
	}
	for _, change := range added.Changes {
		if change.Kind != "added" {
			t.Fatal("empty base was not an addition", change)
		}
	}
	encoded, _ := json.Marshal(added)
	for _, forbidden := range []string{"private-first-password", "private-second-password", "private-account-name", string(private), string(cert), wgKey, peerKey, state.Outbounds[1].Settings.WireGuard.SecretKey, "BEGIN PRIVATE KEY", "BEGIN CERTIFICATE"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("comparison disclosed credential or certificate material")
		}
	}
	cleared := compareStates(state, desiredstate.State{})
	clearedJSON, _ := json.Marshal(cleared)
	for _, change := range cleared {
		if change.Kind != "removed" || change.After != "" {
			t.Fatal("removal did not preserve comparison direction", change)
		}
	}
	if strings.Contains(string(clearedJSON), "private-second-password") || strings.Contains(string(clearedJSON), wgKey) || strings.Contains(string(clearedJSON), peerKey) {
		t.Fatal("removed credentials exposed original values")
	}
	removed, err := repository.Diff(ctx, first.ID, &second.ID)
	if err != nil || len(removed.Changes) != len(diff.Changes) {
		t.Fatal(removed, err)
	}
	if _, err := database.Exec("UPDATE node_runtime_state SET revision=? WHERE node_id=?", second.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	unchanged, err := repository.Diff(ctx, second.ID, nil)
	if err != nil || unchanged.BaseRevision != second.ID || len(unchanged.Changes) != 0 {
		t.Fatal("confirmed runtime version ignored", unchanged, err)
	}
	if _, err := repository.Diff(ctx, second.ID, ptrRevision(999999)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing comparison accepted", err)
	}
	otherNode, _, _, err := nodes.CreateEnrollment(ctx, "Other node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state.NodeID = otherNode.ID
	other, err := repository.Create(ctx, state, "owner", "other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Diff(ctx, second.ID, &other.ID); !errors.Is(err, ErrDiffNode) {
		t.Fatal("cross-node comparison accepted", err)
	}
	var ciphertext []byte
	if err := database.QueryRow("SELECT encrypted_state FROM config_revisions WHERE id=?", second.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	wrongIdentity, err := repository.State(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	wrongIdentity.Revision = other.ID
	wrongData, err := json.Marshal(wrongIdentity)
	if err != nil {
		t.Fatal(err)
	}
	wrongSealed, err := key.Seal(wrongData)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE config_revisions SET encrypted_state=? WHERE id=?", wrongSealed, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Diff(ctx, second.ID, &zero); err == nil {
		t.Fatal("snapshot belonging to another revision produced a comparison")
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := database.Exec("UPDATE config_revisions SET encrypted_state=? WHERE id=?", ciphertext, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Diff(ctx, second.ID, &zero); err == nil {
		t.Fatal("corrupt snapshot produced a comparison")
	}
}

func TestRevisionDiffIncludesTypedQUICOptions(t *testing.T) {
	previous := desiredstate.State{Inbounds: []desiredstate.Inbound{{ID: "quic-entry", Protocol: "hysteria2", Settings: desiredstate.ProtocolSettings{QUIC: &desiredstate.QUICSettings{UpMbps: 20}}}}}
	next := previous
	next.Inbounds = append([]desiredstate.Inbound(nil), previous.Inbounds...)
	next.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{UpMbps: 50}
	changes := compareStates(previous, next)
	if len(changes) != 1 {
		t.Fatal("QUIC change absent from saved field diff", changes)
	}
	data, err := json.Marshal(changes)
	if err != nil || !strings.Contains(string(data), "settings.quic.up_mbps") {
		t.Fatal("typed QUIC field omitted", string(data), err)
	}
}

func TestComparisonReportsNodeNetworkSettingsAddChangeClearAndUnchanged(t *testing.T) {
	previous := desiredstate.State{Revision: 1, Logging: desiredstate.Logging{DomainOnly: true}}
	// A node that never configured the settings has no record, so two states
	// without them stay equal.
	if changes := compareStates(previous, previous); len(changes) != 0 {
		t.Fatal("nil network settings produced changes", changes)
	}
	quoted := func(value string) string {
		data, _ := json.Marshal(value)
		return string(data)
	}
	configured := previous
	configured.Revision = 2
	configured.Network = desiredstate.NetworkOrNil("192.0.2.10", "eth0", "198.51.100.4", "2001:db8::4")
	added := compareStates(previous, configured)
	if len(added) != 4 {
		t.Fatal("network settings addition not reported field by field", added)
	}
	for _, change := range added {
		if change.Category != "network" || change.ID != "node" || change.Kind != "added" || change.Hidden || change.Before != "" {
			t.Fatal("network settings addition misreported", change)
		}
	}
	if changes := compareStates(configured, configured); len(changes) != 0 {
		t.Fatal("unchanged network settings produced changes", changes)
	}
	modified := configured
	modified.Revision = 3
	modified.Network = desiredstate.NetworkOrNil("192.0.2.11", "eth0", "198.51.100.4", "2001:db8::4")
	changes := compareStates(configured, modified)
	if len(changes) != 1 {
		t.Fatal("network settings change not reported field by field", changes)
	}
	if change := changes[0]; change.Category != "network" || change.ID != "node" || change.Kind != "modified" || change.Field != "outbound_source_ip" || change.Before != quoted("192.0.2.10") || change.After != quoted("192.0.2.11") {
		t.Fatal("network settings change misreported", change)
	}
	cleared := compareStates(configured, previous)
	if len(cleared) != 4 {
		t.Fatal("network settings removal not reported field by field", cleared)
	}
	for _, change := range cleared {
		if change.Category != "network" || change.ID != "node" || change.Kind != "removed" || change.After != "" {
			t.Fatal("network settings removal misreported", change)
		}
	}
}

func ptrRevision(value int64) *int64 { return &value }

func TestComparisonStableForResourceOrderPreservesHopOrderAndRemoval(t *testing.T) {
	previous := desiredstate.State{Revision: 1, Outbounds: []desiredstate.Outbound{{ID: "a", Protocol: "direct", Enabled: true}, {ID: "b", Protocol: "block", Enabled: true}}, ProxyChains: []desiredstate.ProxyChain{{ID: "chain", NodeIDs: []string{"a", "b", "c"}, OutboundIDs: []string{"ab", "bc"}}}}
	next := desiredstate.State{Revision: 2, Outbounds: []desiredstate.Outbound{previous.Outbounds[1], previous.Outbounds[0]}, ProxyChains: previous.ProxyChains}
	if diff := compareStates(previous, next); len(diff) != 0 {
		t.Fatal("top-level resource ordering caused changes", diff)
	}
	next.ProxyChains = []desiredstate.ProxyChain{{ID: "chain", NodeIDs: []string{"a", "c", "b"}, OutboundIDs: []string{"ac", "cb"}}}
	next.Outbounds = next.Outbounds[:1]
	diff := compareStates(previous, next)
	if len(diff) != 4 {
		t.Fatal("chain ordering or removal lost", diff)
	}
	for _, change := range diff {
		if change.ID == "a" && change.Kind != "removed" {
			t.Fatal("resource removal not distinguished", change)
		}
	}
	for attempt := 0; attempt < 5; attempt++ {
		if !reflect.DeepEqual(diff, compareStates(previous, next)) {
			t.Fatal("comparison result order unstable")
		}
	}
}
