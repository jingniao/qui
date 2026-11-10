package backup

import (
	"database/sql"
	"testing"
)

// rewindTuningRecommendations removes migration 0141 so a fixture can stand in
// for an archive written before it.
func rewindTuningRecommendations(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0141_tuning_recommendations.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP TABLE node_tuning_dismissals",
		"ALTER TABLE node_metric_samples DROP COLUMN tcp_out_segs",
		"ALTER TABLE node_metric_samples DROP COLUMN tcp_retrans_segs",
		"CREATE TABLE node_meta_old (node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE, region TEXT NOT NULL DEFAULT '', remark TEXT NOT NULL DEFAULT '', renew_date TEXT NOT NULL DEFAULT '', renew_cycle TEXT NOT NULL DEFAULT '', renew_price TEXT NOT NULL DEFAULT '', renew_currency TEXT NOT NULL DEFAULT '', renew_notice_days INTEGER NOT NULL DEFAULT 7 CHECK(renew_notice_days BETWEEN 0 AND 365), last_alert_day TEXT NOT NULL DEFAULT '', port_range_start INTEGER, port_range_end INTEGER CHECK((port_range_start IS NULL AND port_range_end IS NULL) OR (port_range_start IS NOT NULL AND port_range_end IS NOT NULL AND typeof(port_range_start)='integer' AND typeof(port_range_end)='integer' AND port_range_start BETWEEN 1 AND port_range_end AND port_range_end<=65535)))",
		"INSERT INTO node_meta_old(node_id,region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,last_alert_day,port_range_start,port_range_end) SELECT node_id,region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,last_alert_day,port_range_start,port_range_end FROM node_meta",
		"DROP TABLE node_meta",
		"ALTER TABLE node_meta_old RENAME TO node_meta",
		"DELETE FROM schema_migrations WHERE version='0141_tuning_recommendations.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}
