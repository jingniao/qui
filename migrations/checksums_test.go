package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
)

// applied pins the SHA-256 of every migration that may already be applied in a
// deployed database. db.Open and backup validation compare these bytes against
// schema_migrations.checksum, so changing a single byte (including a line
// ending) makes existing panels refuse to start and old backups fail to
// restore. Add new migrations here; never change an existing entry. The
// checksums of the legacy files 0001-0152 live in internal/platform/db.
var applied = map[string]string{
	"0161_agent_state_reset.sql":              "9f9ae4093317b8f86a1cf240f6b43b471c8255a49b234ff35238e3770b06a2fa",
	"0162_telemetry_tiers.sql":                "f3b8d56c5381c82dcaf4accf8ddca1fcadc7a76af65e15de9716c547cb3c9e2b",
	"0163_traffic_minutes.sql":                "041c6d30e1eea7e7f7218c4e8c7fbd569db79d63c4184fa53c5ca07773e16cf9",
	"0160_node_meta_auto_renew.sql":            "afea945dbdf676b704d7e7e1ad85452d4b5ff9b8b2f9d50c413bb0eeb6787c4f",
	"0159_inbound_egress_source.sql":           "e568b6cebfd53dcaa985602d1155c36bc52d57c8a757f6ed998669473b1efe89",
	"0157_agent_system_updates.sql":            "486a6a6e1bbbe2de813d798f7285749d8628efea23cee1ff00f223172988f4ea",
	"0158_subscription_route_dependencies.sql": "ac4ac430cc66e6abdff537ac1fc12df6e5cc9d9549e1e72099a8489e4143ec0a",
	"0156_tuning_module_intent.sql":            "3ab73b186b85ef499db571868851c63536129f44740b1e21690645dfe075fa9c",
	"0155_traffic_display_hours.sql":           "4a30c9c18f70dfc8ec965118c27aaf7dc7f39ac2e9d74911ca41f53f0df34bdb",
	"0154_traffic_storage_retirement.sql":      "b013f88d2f4099a10681e0e05b8a718e3c699792ab86e22b14a4b847bc0f91c6",
	"0152_baseline.sql":                        "2fd1bd9883fb0ce5d6070009aae29bf1f6b6175961c513e2e373b207afc83b4e",
	"0153_drop_one_shot_markers.sql":           "ebd5e3740de73b16e8a6645607ed01d2033ab36cdb034d74200dea8d3c6d7ccd",
}

func TestAppliedMigrationsAreImmutable(t *testing.T) {
	names, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := fs.ReadFile(Files, name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		want, ok := applied[name]
		if !ok {
			t.Errorf("%s is not pinned in applied; add its checksum", name)
			continue
		}
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Errorf("%s changed: checksum %s, pinned %s", name, got, want)
		}
	}
	for name := range applied {
		if _, err := fs.Stat(Files, name); err != nil {
			t.Errorf("pinned migration %s is missing", name)
		}
	}
}
