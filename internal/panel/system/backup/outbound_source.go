package backup

import (
	"context"
	"database/sql"
	"google.golang.org/protobuf/encoding/protojson"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

func validateOutboundSource(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, "SELECT protocol,source_ip,source_interface FROM outbounds")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var protocol, ip, iface string
		if rows.Scan(&protocol, &ip, &iface) != nil {
			return ErrArchive
		}
		if (desiredstate.Network{OutboundSourceIP: ip, OutboundInterface: iface}).Validate() != nil {
			return ErrArchive
		}
		if strings.EqualFold(protocol, "block") && (ip != "" || iface != "") {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

// validateInboundEgress skips archives that predate the inbound egress column.
func validateInboundEgress(ctx context.Context, database *sql.DB) error {
	// validateMigrations already checked the recorded versions and checksums.
	var applied bool
	if err := database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", "0159_inbound_egress_source.sql").Scan(&applied); err != nil {
		return ErrArchive
	}
	if !applied {
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT i.core,i.egress_source_ip,n.capabilities_json FROM inbounds i JOIN nodes n ON n.id=i.node_id")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var core, ip string
		var raw []byte
		if rows.Scan(&core, &ip, &raw) != nil {
			return ErrArchive
		}
		if core == "" && ip != "" {
			var caps pb.Capabilities
			if (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, &caps) != nil {
				return ErrArchive
			}
			core = caps.CoreName
		}
		canonical, err := desiredstate.CanonicalEgressSource(ip)
		if err != nil || canonical != ip || core == desiredstate.CoreGost && ip != "" {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}
