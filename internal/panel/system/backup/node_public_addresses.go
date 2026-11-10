package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"strings"
	"time"
)

func validateNodePublicAddresses(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0138_node_public_addresses.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT public_addresses_json, public_ip_selected FROM nodes")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var raw, selected string
		if err := rows.Scan(&raw, &selected); err != nil {
			return ErrArchive
		}
		var list []struct {
			Address  string `json:"address"`
			Source   string `json:"source"`
			Family   string `json:"family"`
			LastSeen string `json:"lastSeen"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return ErrArchive
		}
		if len(list) > 16 {
			return ErrArchive
		}
		for _, addr := range list {
			ip := net.ParseIP(addr.Address)
			if ip == nil {
				return ErrArchive
			}
			if addr.Family != "ipv4" && addr.Family != "ipv6" {
				return ErrArchive
			}
			if addr.Source != "peer" && !strings.HasPrefix(addr.Source, "interface:") {
				return ErrArchive
			}
			if addr.LastSeen != "" {
				if _, err := time.Parse(time.RFC3339, addr.LastSeen); err != nil {
					return ErrArchive
				}
			}
		}
		if selected != "" && net.ParseIP(selected) == nil {
			return ErrArchive
		}
	}
	if err := rows.Err(); err != nil {
		return ErrArchive
	}
	return nil
}
