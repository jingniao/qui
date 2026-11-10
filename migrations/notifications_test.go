package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNotificationsMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	apply(t, database, "0139_notifications.sql")

	var count int
	if err := database.QueryRow("SELECT count(*) FROM notification_rules").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Fatalf("expected 7 default rules, got %d", count)
	}

	// Verify CHECK constraint rejects unknown kind on notification_rules
	if _, err := database.Exec("INSERT INTO notification_rules(kind,enabled,threshold,duration_minutes,cooldown_minutes,updated_at) VALUES('unknown',1,0,0,30,'2026-11-09T00:00:00Z')"); err == nil {
		t.Fatal("expected CHECK constraint violation on unknown rule kind")
	}

	// Verify CHECK constraint on notification_channels kind
	if _, err := database.Exec("INSERT INTO notification_channels(id,kind,name,enabled,encrypted_config,created_at,updated_at) VALUES('ch1','email','Mail',1,X'00','2026-11-09T00:00:00Z','2026-11-09T00:00:00Z')"); err == nil {
		t.Fatal("expected CHECK constraint violation on unknown channel kind")
	}

	// Verify CHECK constraint on notifications severity
	if _, err := database.Exec("INSERT INTO notifications(id,at,kind,severity,title,body) VALUES(1,'2026-11-09T00:00:00Z','test','fatal','Title','Body')"); err == nil {
		t.Fatal("expected CHECK constraint violation on unknown severity")
	}
}
