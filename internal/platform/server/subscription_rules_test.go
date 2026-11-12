package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/system"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/testutil/dbtest"
)

// A2.3: the client rule template travels with a backup. The template saved
// before the backup comes back from the archive even after the running panel
// replaced it.
func TestClientRuleTemplateBackupRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	app := testServer(t)
	owner := budgetOwner(t, app)
	saved := rules.Presets()[3].Template
	sets := &policy.Repository{Database: app.Database, ClientReferences: clientServiceReferences{}}
	input := policy.ServiceInput{Name: "备份共享规则", Enabled: true, Domains: []string{"example.org"}}
	set, err := sets.SaveService(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	saved.Rules = append([]rules.Rule{{ID: "shared", Type: rules.RuleSet, Payload: "service:" + set.ID, Target: "DIRECT", Enabled: true, Priority: 1}}, saved.Rules...)
	if response := owner.call("PUT", "/api/v1/subscription-rules", map[string]any{"template": saved, "expectedRevision": 0}); response.Code != 200 {
		t.Fatalf("save template: %d %s", response.Code, response.Body.String())
	}
	input.Enabled = false
	if _, err := sets.SaveService(ctx, set.ID, input); err != nil {
		t.Fatal(err)
	}
	const password = "rules-backup-password"
	created := owner.call("POST", "/api/v1/backups", map[string]any{"password": password})
	var backup system.Backup
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &backup) != nil {
		t.Fatalf("backup: %d", created.Code)
	}
	if response := owner.call("PUT", "/api/v1/subscription-rules", map[string]any{"template": rules.Presets()[1].Template, "expectedRevision": 1}); response.Code != 200 {
		t.Fatalf("second save: %d", response.Code)
	}
	download := owner.call("GET", "/api/v1/backups/"+backup.ID+"/download", nil)
	if download.Code != 200 {
		t.Fatalf("download: %d", download.Code)
	}
	archivePath := filepath.Join(t.TempDir(), "rules.pcb")
	if err := os.WriteFile(archivePath, download.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	panel := config.DefaultPanel()
	panel.Database = filepath.Join(target, "panel.db")
	panel.NodeIdentityDirectory = filepath.Join(target, "identity")
	prepared, err := archive.PrepareRestore(ctx, t.TempDir(), archivePath, password, archive.RestoreTarget{Panel: panel, ConfigurationPath: filepath.Join(target, "panel.yaml")})
	if err != nil {
		t.Fatalf("prepare restore: %v", err)
	}
	defer archive.DiscardPreparedRestore(ctx, prepared)
	restored, err := dbtest.Open(ctx, filepath.Join(prepared.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	stored, err := (&rules.Store{Database: restored}).Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 1 || stored.Template.Preset != "whitelist" || len(stored.Template.Rules) != len(saved.Rules) {
		t.Fatalf("restored template: revision %d preset %q", stored.Revision, stored.Template.Preset)
	}
	profiles := &rules.Store{Database: restored}
	list, err := profiles.Profiles(ctx, 1, 100)
	if err != nil || list.Total != 1 || list.ActiveRevision != stored.Revision {
		t.Fatal(list, err)
	}
	active, err := profiles.Profile(ctx, list.ActiveProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := rules.Equal(active.Profile.Template, stored.Template); err != nil || !same {
		t.Fatal("restored profile differs from active snapshot", err)
	}
	refs, err := (&policy.Repository{Database: restored, ClientReferences: clientServiceReferences{}}).ServiceReferences(ctx, set.ID)
	if err != nil || len(refs.ClientProfiles) != 1 || !refs.ClientProfiles[0].Active || len(refs.ClientProfiles[0].Rules) != 1 || refs.ClientProfiles[0].Rules[0].ID != "shared" {
		t.Fatal(refs, err)
	}
}
