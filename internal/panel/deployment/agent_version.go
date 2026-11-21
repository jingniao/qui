package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
)

const MinimumCoreOptionalAgentVersion = "1.9.0"

var ErrAgentVersionUnsupported = errors.New("agent_version_unsupported")

func requireAgentVersion(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID, version string) error {
	var raw []byte
	if err := db.QueryRowContext(ctx, "SELECT capabilities_json FROM nodes WHERE id=?", nodeID).Scan(&raw); err != nil {
		return err
	}
	c := &pb.Capabilities{}
	if err := storedJSON.Unmarshal(raw, c); err != nil {
		return err
	}
	if !node.CoreOptional(c) {
		return nil
	}
	// A release candidate of the minimum (1.9.0-rc.N) already carries the
	// feature, so only the X.Y.Z part is compared.
	core, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), "-rc.")
	parts := strings.Split(core, ".")
	minimum := strings.Split(MinimumCoreOptionalAgentVersion, ".")
	if len(parts) != len(minimum) {
		return fmt.Errorf("%w: target %q must support optional user cores (>= %s)", ErrAgentVersionUnsupported, version, MinimumCoreOptionalAgentVersion)
	}
	var values, floors [3]uint64
	for i, part := range parts {
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid target %q: %w", ErrAgentVersionUnsupported, version, err)
		}
		floor, err := strconv.ParseUint(minimum[i], 10, 64)
		if err != nil {
			return err
		}
		values[i], floors[i] = value, floor
	}
	for i, value := range values {
		floor := floors[i]
		if value > floor {
			return nil
		}
		if value < floor {
			return fmt.Errorf("%w: node without a user core requires Agent >= %s", ErrAgentVersionUnsupported, MinimumCoreOptionalAgentVersion)
		}
	}
	return nil
}
