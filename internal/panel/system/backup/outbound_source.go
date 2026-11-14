package backup

import (
	"context"
	"database/sql"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

func validateOutboundSource(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0145_outbound_source.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
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
