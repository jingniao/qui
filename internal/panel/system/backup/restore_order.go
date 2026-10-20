package backup

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Ordering is derived from authenticated mappings, never an installation
// permission. Preserve original File bytes/digests and destination exactly.
func OrderRestoreFiles(files []RestoreFile) ([]RestoreFile, error) {
	if len(files) == 0 || len(files) > 2048 {
		return nil, ErrRestoreStage
	}
	ordered := make([]RestoreFile, 0, len(files))
	seenNames := map[string]bool{}
	seenTargets := map[string]bool{}
	databases, configs := 0, 0
	for _, f := range files {
		if f.Destination == "" {
			continue
		}
		targetKey := f.Destination
		if runtime.GOOS == "windows" {
			targetKey = strings.ToLower(targetKey)
		}
		if !filepath.IsAbs(f.Destination) || filepath.Clean(f.Destination) != f.Destination || seenNames[f.Name] || seenTargets[targetKey] {
			return nil, ErrRestoreStage
		}
		if _, _, ok := restoreInstallCategory(f.Name); !ok {
			return nil, ErrRestoreStage
		}
		if f.Name == "database/panel.db" {
			databases++
		}
		if f.Name == restoreConfigName {
			configs++
		}
		seenNames[f.Name] = true
		seenTargets[targetKey] = true
		ordered = append(ordered, f)
	}
	if databases != 1 || configs != 1 {
		return nil, ErrRestoreStage
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, _, _ := restoreInstallCategory(ordered[i].Name)
		b, _, _ := restoreInstallCategory(ordered[j].Name)
		if a != b {
			return a < b
		}
		return ordered[i].Name < ordered[j].Name
	})
	return ordered, nil
}
func restoreInstallCategory(name string) (int, string, bool) {
	switch name {
	case "identity/configuration.key", "identity/ca.key", "identity/server.key", "tls/server.key":
		return 10, "keys", true
	case "identity/ca.crt", "identity/server.crt", "tls/server.crt", "tls/client-ca.crt":
		return 20, "certificates", true
	case "identity/node-ca-active.enc", "identity/node-ca-retired.enc", "identity/node-ca-round-active.enc", "identity/node-ca-round-retired.enc", "identity/node-ca-recovered-base.enc", "identity/node-ca-recovered-round.enc", "identity/node-ca-recovered-retired.enc":
		return 30, "identity_history", true
	case "database/panel.db":
		return 50, "database", true
	case restoreConfigName:
		return 60, "target_configuration", true
	default:
		if strings.HasPrefix(name, "rulesets/") && validName(name) {
			return 40, "rulesets", true
		}
		return 0, "", false
	}
}
