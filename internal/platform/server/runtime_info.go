package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// Owner-only metadata, never returns config contents, private keys or pins.
func (s *Server) runtimeInfo(ctx context.Context) (any, error) {
	var sqliteVersion, journal, migration string
	if err := s.Database.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&sqliteVersion); err != nil {
		return nil, err
	}
	if err := s.Database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		return nil, err
	}
	if err := s.Database.QueryRowContext(ctx, "SELECT coalesce(max(version),'') FROM schema_migrations").Scan(&migration); err != nil {
		return nil, err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	buildVersion := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		buildVersion = info.Main.Version
	}
	var databaseBytes int64
	if info, err := os.Stat(s.Config.Database); err == nil {
		databaseBytes = info.Size()
	}
	certificate := map[string]any{"configured": s.Config.TLS.Certificate != "", "available": false}
	if data, err := os.ReadFile(s.Config.TLS.Certificate); err == nil {
		if block, _ := pem.Decode(data); block != nil {
			if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
				certificate["available"] = true
				certificate["notBefore"] = cert.NotBefore
				certificate["notAfter"] = cert.NotAfter
				certificate["validNow"] = !time.Now().Before(cert.NotBefore) && time.Now().Before(cert.NotAfter)
			}
		}
	}
	return map[string]any{
		"generatedAt": time.Now().UTC(), "startedAt": s.startedAt, "uptimeSeconds": int64(time.Since(s.startedAt).Seconds()),
		"goVersion": runtime.Version(), "buildVersion": buildVersion, "os": runtime.GOOS, "arch": runtime.GOARCH,
		"goroutines": runtime.NumGoroutine(), "heapBytes": memory.HeapAlloc, "databaseBytes": databaseBytes,
		"sqliteVersion": sqliteVersion, "journalMode": journal, "migration": migration,
		"network":  map[string]any{"development": s.Config.Development, "httpAddress": s.HTTP.Addr, "grpcAddress": s.Config.GRPCAddress, "tlsEnabled": !s.Config.Development, "minimumTLS": "1.3", "certificate": certificate},
		"security": map[string]any{"secureCookies": !s.Config.Development, "httpOnlyCookies": true, "sameSite": "Strict", "csrfRequired": true, "passwordMinCharacters": 12, "passwordMaxCharacters": 128, "passwordAlgorithm": "PBKDF2-SHA256", "passwordIterations": 600000, "loginAttempts": 5, "loginWindowMinutes": 15, "requestBytes": 1 << 20},
	}, nil
}
