package proxy

import (
	"reflect"
	"testing"
)

func TestImportedProxyCanvasesDraftAndPublished(t *testing.T) {
	p := newCanvasPanel(t)
	for _, row := range []struct{ id, draft, published string }{
		{"draft", `{"instances":[{"instanceId":"a","ref":"import:target"}]}`, ""},
		{"published", `{}`, `{"instances":[{"instanceId":"a","ref":"import:target"}]}`},
		{"both", `{"instances":[{"instanceId":"a","ref":"import:target"}]}`, `{"instances":[{"instanceId":"b","ref":"import:target"}]}`},
		{"other", `{"instances":[{"instanceId":"a","ref":"import:target-more"}]}`, ""},
	} {
		if _, err := p.database.ExecContext(p.ctx, "INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at) VALUES(?,?,'client',?,?,'now','now')", row.id, row.id, row.draft, row.published); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := p.database.BeginTx(p.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := ImportedProxyCanvasesTx(p.ctx, tx, "target")
	want := []ImportCanvasUsage{{"both", "both"}, {"draft", "draft"}, {"published", "published"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("usage=%v error=%v", got, err)
	}
	if _, err := tx.ExecContext(p.ctx, "UPDATE canvas_schemes SET client_published_layout='invalid' WHERE id='other'"); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportedProxyCanvasesTx(p.ctx, tx, "target"); err == nil {
		t.Fatal("corrupt snapshot accepted")
	}
}
