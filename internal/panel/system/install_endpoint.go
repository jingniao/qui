package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"

	"proxycontrol/pkg/contracts/netaddr"
)

var ErrInstallEndpointInvalid = errors.New("invalid node install endpoint (hosts must be DNS names or IPv4 literals without a scheme, ports 1 to 65535 or 0 for unset, and the Web and gRPC ports must differ)")
var ErrInstallEndpointConflict = errors.New("node install endpoint changed; refresh before saving")

// installEndpointKey is the single system_settings row this feature owns. An
// absent row means "unset": the install dialogs then fall back to the address
// the browser used to reach the panel, which is the behaviour before v1.4.2.
const installEndpointKey = "install_endpoint"

// hostPattern is the rule the frontend uses before it builds the command and the
// rule deploy/linux/node-install.sh applies to --panel. A bare IPv6 literal
// contains ':' and therefore fails both, so it is refused instead of storing a
// value the install command can never use. The rule itself lives in netaddr.

// InstallEndpoint is the panel address one-click Agent installation defaults to:
// the domain or IPv4 literal a new node can reach plus the panel Web (HTTPS)
// port and, optionally, the Agent gRPC address and port. The gRPC values can be
// saved for a panel whose gRPC listener is reached through a different name or a
// port forward; empty and 0 keep the earlier behaviour and use the panel address
// and the port the panel actually listens on.
type InstallEndpoint struct {
	Host      string `json:"host"`
	HTTPPort  int    `json:"httpPort"`
	GRPCHost  string `json:"grpcHost"`
	GRPCPort  int    `json:"grpcPort"`
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

// InstallEndpointInput is the write shape. All four values are independently
// optional: an Owner who only knows one value can save it alone and the dialogs
// keep the browser/server default for the rest.
type InstallEndpointInput struct {
	Host             string `json:"host"`
	HTTPPort         int    `json:"httpPort"`
	GRPCHost         string `json:"grpcHost"`
	GRPCPort         int    `json:"grpcPort"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// validateInstallEndpoint accepts empty hosts (unset) and otherwise DNS names
// or IPv4 literals that the installer's `--panel`/`--grpc-host` checks accept.
// Label rules are stricter than node-install.sh's single regular expression on
// purpose: a value that fails here could never install a node, so refusing it
// early is the only honest answer. 0 means "no saved port" for each port, and
// an empty gRPC host means "same as the panel host". Two saved ports that are
// equal are refused for the same reason node-install.sh refuses them: the Web
// listener and the gRPC listener cannot share one port, so such a command could
// never install a node.
func validateInstallEndpoint(host string, httpPort, grpcPort int, grpcHost string) error {
	if httpPort < 0 || httpPort > 65535 || grpcPort < 0 || grpcPort > 65535 {
		return ErrInstallEndpointInvalid
	}
	if httpPort != 0 && httpPort == grpcPort {
		return ErrInstallEndpointInvalid
	}
	if !validInstallHost(host) || !validInstallHost(grpcHost) {
		return ErrInstallEndpointInvalid
	}
	return nil
}

// validInstallHost reports whether one saved host can be used verbatim in the
// install command: empty (unset) or a DNS name / IPv4 literal without scheme,
// port or brackets. The label rules live in netaddr, which the node connect
// address shares; a bare IPv6 literal is refused here because
// deploy/linux/node-install.sh cannot take one for --panel.
func validInstallHost(host string) bool {
	return host == "" || netaddr.ValidHostName(host)
}

func readInstallEndpoint(ctx context.Context, tx *sql.Tx) (InstallEndpoint, error) {
	endpoint := InstallEndpoint{}
	var encoded string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", installEndpointKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return endpoint, nil
	}
	if err != nil {
		return endpoint, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&endpoint); err != nil {
		return InstallEndpoint{}, ErrInstallEndpointInvalid
	}
	if decoder.Decode(&struct{}{}) != io.EOF || endpoint.Revision < 1 {
		return InstallEndpoint{}, ErrInstallEndpointInvalid
	}
	if validateInstallEndpoint(endpoint.Host, endpoint.HTTPPort, endpoint.GRPCPort, endpoint.GRPCHost) != nil {
		return InstallEndpoint{}, ErrInstallEndpointInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, endpoint.UpdatedAt); err != nil {
		return InstallEndpoint{}, ErrInstallEndpointInvalid
	}
	return endpoint, nil
}

func (r *Repository) ReadInstallEndpoint(ctx context.Context) (InstallEndpoint, error) {
	tx, err := r.reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return InstallEndpoint{}, err
	}
	defer tx.Rollback()
	endpoint, err := readInstallEndpoint(ctx, tx)
	if err != nil {
		return endpoint, err
	}
	return endpoint, tx.Commit()
}

// SaveInstallEndpoint writes the endpoint under one revision, like SaveBase and
// SaveStorageThresholds: an unchanged save keeps the stored revision and writes
// no audit row, a stale expectedRevision is a conflict, and clearing a saved
// value (host empty, ports 0) is a real change worth an audit row.
func (r *Repository) SaveInstallEndpoint(ctx context.Context, input InstallEndpointInput, actor, ip string) (InstallEndpoint, bool, error) {
	host, grpcHost := strings.TrimSpace(input.Host), strings.TrimSpace(input.GRPCHost)
	if validateInstallEndpoint(host, input.HTTPPort, input.GRPCPort, grpcHost) != nil || input.ExpectedRevision < 0 || actor == "" {
		return InstallEndpoint{}, false, ErrInstallEndpointInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return InstallEndpoint{}, false, err
	}
	defer tx.Rollback()
	current, err := readInstallEndpoint(ctx, tx)
	if err != nil {
		return current, false, err
	}
	if host == current.Host && input.HTTPPort == current.HTTPPort && grpcHost == current.GRPCHost && input.GRPCPort == current.GRPCPort {
		return current, false, nil
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return current, false, ErrInstallEndpointConflict
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	next := InstallEndpoint{Host: host, HTTPPort: input.HTTPPort, GRPCHost: grpcHost, GRPCPort: input.GRPCPort, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: actor}
	data, err := json.Marshal(next)
	if err != nil {
		return next, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", installEndpointKey, string(data)); err != nil {
		return next, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改节点安装连接地址',?,'成功',?)", stamp, actor, installEndpointKey, ip); err != nil {
		return next, false, err
	}
	if err := tx.Commit(); err != nil {
		return next, false, err
	}
	return next, true, nil
}
