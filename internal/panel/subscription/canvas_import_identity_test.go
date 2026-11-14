package subscription

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	panelproxy "proxycontrol/internal/panel/proxy"
)

func TestReconcileCanvasImportRefsBothSnapshots(t *testing.T) {
	p := setup(t)
	ctx := context.Background()
	const raw = `{"instances":[{"instanceId":"one","ref":"import:old","x":1,"y":2,"role":"entry"},{"instanceId":"two","ref":"import:url","x":3,"y":4,"role":"exit"},{"instanceId":"lost","ref":"import:missing","x":5,"y":6,"role":"relay"}],"connections":[{"id":"edge","source":"one","target":"two","type":"proxy"}]}`
	const stamp = "2026-11-13T01:02:03.000000000Z"
	if _, err := p.database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at) VALUES('identity','identity','client',?,?,'before',?)", raw, raw, stamp); err != nil {
		t.Fatal(err)
	}
	tx, err := p.database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	report, err := panelproxy.ReconcileImportRefsTx(ctx, tx, func(old string) (string, bool) {
		switch old {
		case "old":
			return "new", true
		case "url":
			return "url", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 2 || !strings.Contains(strings.Join(report, " "), "missing") {
		t.Fatalf("report=%v", report)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var draft, published, updated string
	if err := p.database.QueryRow("SELECT layout_json,client_published_layout,updated_at FROM canvas_schemes WHERE id='identity'").Scan(&draft, &published, &updated); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{draft, published} {
		var layout panelproxy.CanvasLayout
		if err := json.Unmarshal([]byte(data), &layout); err != nil {
			t.Fatal(err)
		}
		if len(layout.Instances) != 3 || layout.Instances[0].Ref != "import:new" || layout.Instances[1].Ref != "import:url" || layout.Instances[2].Ref != "import:missing" || layout.Instances[0].InstanceID != "one" || layout.Instances[0].X != 1 || len(layout.Connections) != 1 || layout.Connections[0].Source != "one" || layout.Connections[0].Target != "two" {
			t.Fatalf("layout=%+v", layout)
		}
	}
	if updated <= stamp {
		t.Fatalf("concurrency stamp did not advance: %s", updated)
	}
}
