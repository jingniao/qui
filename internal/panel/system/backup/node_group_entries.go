package backup

import (
	"context"
	"database/sql"
	"strconv"
)

func validateNodeGroupEntries(ctx context.Context, database *sql.DB) error {
	importLimit := 1000
	for _, q := range []string{
		"SELECT count(*) FROM node_meta WHERE NOT ((port_range_start IS NULL AND port_range_end IS NULL) OR (port_range_start IS NOT NULL AND port_range_end IS NOT NULL AND typeof(port_range_start)='integer' AND typeof(port_range_end)='integer' AND port_range_start BETWEEN 1 AND port_range_end AND port_range_end<=65535))",
		"SELECT count(*) FROM node_group_servers e LEFT JOIN node_groups g ON g.id=e.group_id LEFT JOIN nodes n ON n.id=e.node_id WHERE g.id IS NULL OR n.id IS NULL OR n.deleted_at IS NOT NULL",
		"SELECT count(*) FROM node_group_inbounds e LEFT JOIN node_groups g ON g.id=e.group_id LEFT JOIN inbounds i ON i.id=e.inbound_id WHERE g.id IS NULL OR i.id IS NULL",
		"SELECT count(*) FROM node_group_imports e LEFT JOIN node_groups g ON g.id=e.group_id LEFT JOIN subscription_sources s ON s.id=e.source_id WHERE g.id IS NULL OR s.id IS NULL OR e.proxy_key IS NULL OR length(e.proxy_key)>1024",
		"SELECT count(*) FROM (SELECT group_id FROM node_group_servers GROUP BY group_id HAVING count(*)>100)",
		"SELECT count(*) FROM (SELECT group_id FROM node_group_inbounds GROUP BY group_id HAVING count(*)>500)",
		"SELECT count(*) FROM (SELECT group_id FROM node_group_imports GROUP BY group_id HAVING count(*)>" + strconv.Itoa(importLimit) + ")",
		"SELECT count(*) FROM (SELECT m.group_id,m.node_id FROM node_group_members m EXCEPT SELECT group_id,node_id FROM node_group_servers EXCEPT SELECT e.group_id,i.node_id FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id)",
		"SELECT count(*) FROM (SELECT group_id,node_id FROM node_group_servers UNION SELECT e.group_id,i.node_id FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id EXCEPT SELECT m.group_id,m.node_id FROM node_group_members m)",
	} {
		var n int
		if database.QueryRowContext(ctx, q).Scan(&n) != nil || n != 0 {
			return ErrArchive
		}
	}
	return nil
}
