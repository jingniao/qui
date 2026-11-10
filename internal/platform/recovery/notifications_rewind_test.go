package recovery

import (
	"database/sql"
	"testing"
)

// rewindNotifications removes migration 0139 so a fixture can stand in for
// an archive written before it.
func rewindNotifications(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindServiceSetSources(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0139_notifications.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DROP TABLE notification_state",
		"DROP TABLE notifications",
		"DROP TABLE notification_channels",
		"DROP TABLE notification_rules",
		"DELETE FROM schema_migrations WHERE version='0139_notifications.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}
