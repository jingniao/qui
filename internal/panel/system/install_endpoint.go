package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
)

var ErrInstallEndpointInvalid = errors.New("invalid node install endpoint (host must be a DNS name or IPv4 literal without a scheme, port 1 to 65535 or 0 for unset)")
var ErrInstallEndpointConflict = errors.New("node install endpoint changed; refresh before saving")

// installEndpointKey is the single system_settings row this feature owns. An
// absent row means "unset": the install dialogs then fall back to the address
// the browser used to reach the panel, which is the behaviour before v1.4.2.
const installEndpointKey = "install_endpoint"

// hostPattern is the rule the frontend uses before it builds the command and the
// rule deploy/linux/node-install.sh applies to --panel. A bare IPv6 literal
// contains ':' and therefore fails both, so it is refused here as well instead
// of storing a value the install command can never use.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// InstallEndpoint is the panel address one-click Agent installation defaults to:
// the domain or IPv4 literal a new node can reach plus the panel Web (HTTPS)
// port. The Agent gRPC port is not part of it — the enrollment response carries
// the port the panel actually listens on.
type InstallEndpoint struct {
	Host      string `json:"host"`
	HTTPPort  int    `json:"httpPort"`
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

// InstallEndpointInput is the write shape. Host and HTTPPort are independently
// optional: an Owner who only knows the port can leave the host empty and the
// dialogs keep the browser hostname while using the saved port.
type InstallEndpointInput struct {
	Host             string `json:"host"`
	HTTPPort         int    `json:"httpPort"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// validateInstallEndpoint accepts an empty host (unset) and otherwise a DNS name
// or IPv4 literal that the installer's `--panel` check accepts. Label rules are
// stricter than node-install.sh's single regular expression on purpose: a value
// that fails here could never install a node, so refusing it early is the only
// honest answer. 0 means "no saved port".
func validateInstallEndpoint(host string, port int) error {
	if port < 0 || port > 65535 {
		return ErrInstallEndpointInvalid
	}
	if host == "" {
		return nil
	}
	if len(host) > 253 || !hostPattern.MatchString(host) {
		return ErrInstallEndpointInvalid
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return ErrInstallEndpointInvalid
		}
	}
	return nil
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
	if validateInstallEndpoint(endpoint.Host, endpoint.HTTPPort) != nil {
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
// value (host empty, port 0) is a real change worth an audit row.
func (r *Repository) SaveInstallEndpoint(ctx context.Context, input InstallEndpointInput, actor, ip string) (InstallEndpoint, bool, error) {
	host := strings.TrimSpace(input.Host)
	if validateInstallEndpoint(host, input.HTTPPort) != nil || input.ExpectedRevision < 0 || actor == "" {
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
	if host == current.Host && input.HTTPPort == current.HTTPPort {
		return current, false, nil
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return current, false, ErrInstallEndpointConflict
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	next := InstallEndpoint{Host: host, HTTPPort: input.HTTPPort, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: actor}
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
