package backup

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanvasEntryBaselineArchiveValidation(t *testing.T) {
	fields := map[string]string{}
	for _, key := range []string{"名称", "协议", "监听地址", "端口", "核心", "协议设置", "启用", "订阅可见", "允许用户"} {
		fields[key] = strings.Repeat("a", 64)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(data)
	for _, tc := range []struct {
		name           string
		config, groups any
		valid          bool
	}{
		{"historical", nil, nil, true}, {"baseline", valid, `["a","b"]`, true},
		{"empty-groups", valid, `[]`, true}, {"partial", valid, nil, false},
		{"short-digest", strings.Replace(valid, strings.Repeat("a", 64), "bad", 1), `[]`, false},
		{"missing-field", `{}`, `[]`, false}, {"null-groups", valid, `null`, false},
		{"unsorted", valid, `["b","a"]`, false}, {"duplicate", valid, `["a","a"]`, false},
		{"noncanonical", valid, `[ "a" ]`, false}, {"empty-id", valid, `[""]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := canvasBindingArchiveDB(t)
			for _, q := range []string{"INSERT INTO schema_migrations VALUES('0148_canvas_entry_baseline.sql')", "ALTER TABLE canvas_entry_bindings ADD COLUMN creation_config_json TEXT", "ALTER TABLE canvas_entry_bindings ADD COLUMN creation_groups_json TEXT"} {
				if _, err := database.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := database.Exec("UPDATE canvas_entry_bindings SET creation_config_json=?,creation_groups_json=?", tc.config, tc.groups); err != nil {
				t.Fatal(err)
			}
			if err := validateCanvasEntryBindings(context.Background(), database); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
