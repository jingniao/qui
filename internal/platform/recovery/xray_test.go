package recovery

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/vault"
	"strings"
	"testing"
)

func queueObserver(t *testing.T, directory string) (*sql.DB, *vault.Vault) {
	t.Helper()
	absolute, err := filepath.Abs(filepath.Join(directory, "spool", "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(absolute)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "busy_timeout(5000)"}}.Encode()}
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	keyBytes, err := os.ReadFile(filepath.Join(directory, "spool", "spool.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := vault.FromKey(keyBytes)
	clear(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	return database, key
}
