package proxy

import (
	"encoding/json"
	"fmt"
	"testing"
)

func assertCanvasArrayJSON(t *testing.T, raw string) {
	t.Helper()
	var layout map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &layout); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"instances", "connections"} {
		if string(layout[key]) != "[]" {
			t.Fatalf("%s must be [], got %s in %s", key, layout[key], raw)
		}
	}
}

func TestCanvasEmptyArraysCreateUpdateAndRead(t *testing.T) {
	for _, execution := range []string{"server", "client"} {
		for _, layout := range []string{`{}`, `{"instances":[],"connections":[]}`, `{"instances":[]}`, `{"instances":null,"connections":null}`} {
			t.Run(execution+layout, func(t *testing.T) {
				p := newCanvasPanel(t)
				cookie, csrf := p.session("Owner")
				response := p.call("POST", "/api/v1/canvas-schemes", fmt.Sprintf(`{"name":"empty","execution":%q,"layout":%s}`, execution, layout), cookie, csrf)
				if response.Code != 201 {
					t.Fatalf("create: %d %s", response.Code, response.Body.String())
				}
				var body struct {
					ID        string          `json:"id"`
					UpdatedAt string          `json:"updatedAt"`
					Layout    json.RawMessage `json:"layout"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				assertCanvasArrayJSON(t, string(body.Layout))
				checkStored := func() {
					var raw string
					if err := p.database.QueryRowContext(p.ctx, "SELECT layout_json FROM canvas_schemes WHERE id=?", body.ID).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					assertCanvasArrayJSON(t, raw)
				}
				checkStored()
				response = p.call("PUT", "/api/v1/canvas-schemes/"+body.ID, fmt.Sprintf(`{"name":"empty","execution":%q,"layout":%s,"updatedAt":%q}`, execution, layout, body.UpdatedAt), cookie, csrf)
				if response.Code != 200 {
					t.Fatalf("update: %d %s", response.Code, response.Body.String())
				}
				checkStored()
				// Simulate an old row, without repairing it in storage on GET.
				legacy := `{"instances":null,"connections":null}`
				if _, err := p.database.ExecContext(p.ctx, "UPDATE canvas_schemes SET layout_json=? WHERE id=?", legacy, body.ID); err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{"", "/" + body.ID} {
					response = p.call("GET", "/api/v1/canvas-schemes"+path, nil, cookie, csrf)
					if response.Code != 200 {
						t.Fatalf("read: %d %s", response.Code, response.Body.String())
					}
					if path == "" {
						var page struct {
							Items []struct {
								Layout json.RawMessage `json:"layout"`
							} `json:"items"`
						}
						if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
							t.Fatal(err)
						}
						if len(page.Items) != 1 {
							t.Fatalf("items: %s", response.Body.String())
						}
						assertCanvasArrayJSON(t, string(page.Items[0].Layout))
					} else {
						if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
							t.Fatal(err)
						}
						assertCanvasArrayJSON(t, string(body.Layout))
					}
				}
				stored, err := p.service.Repository.CanvasScheme(p.ctx, body.ID)
				if err != nil || stored.Layout.Instances == nil || stored.Layout.Connections == nil {
					t.Fatalf("read normalization: %+v, %v", stored, err)
				}
				var raw string
				if err := p.database.QueryRowContext(p.ctx, "SELECT layout_json FROM canvas_schemes WHERE id=?", body.ID).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				if raw != legacy {
					t.Fatalf("GET changed historical row: %s", raw)
				}
			})
		}
	}
}

func TestCanvasLayoutEncodingAlwaysUsesArrays(t *testing.T) {
	for _, layout := range []CanvasLayout{{}, {Instances: []CanvasInstance{}}, {Connections: []CanvasConnection{}}} {
		raw, err := json.Marshal(layout)
		if err != nil {
			t.Fatal(err)
		}
		assertCanvasArrayJSON(t, string(raw))
	}
	if _, err := decodeCanvasLayout([]byte(`{"instances":[],"connections":[],"unknown":true}`)); err == nil {
		t.Fatal("normalization must retain strict layout decoding")
	}
}

func TestCanvasClientSnapshotAndRecoveryNormalizeHistoricalArrays(t *testing.T) {
	p := newCanvasPanel(t)
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "client", Execution: "client"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"instances":null,"connections":null}`
	if _, err := p.database.ExecContext(p.ctx, "UPDATE canvas_schemes SET layout_json=? WHERE id=?", legacy, scheme.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.PublishClientCanvas(p.ctx, scheme); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := p.database.QueryRowContext(p.ctx, "SELECT client_published_layout FROM canvas_schemes WHERE id=?", scheme.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	assertCanvasArrayJSON(t, raw)
	if _, err := p.database.ExecContext(p.ctx, "ATTACH DATABASE ':memory:' AS restore_source"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.ExecContext(p.ctx, "CREATE TABLE restore_source.canvas_schemes(id TEXT, execution TEXT, client_published_layout TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.ExecContext(p.ctx, "INSERT INTO restore_source.canvas_schemes VALUES(?,'client',?)", scheme.ID, legacy); err != nil {
		t.Fatal(err)
	}
	tx, err := p.database.BeginTx(p.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := p.service.Repository.PrepareClientRestoreTx(p.ctx, tx); err != nil {
		t.Fatal(err)
	}
	var draft string
	if err := tx.QueryRowContext(p.ctx, "SELECT layout_json,client_published_layout FROM canvas_schemes WHERE id=?", scheme.ID).Scan(&draft, &raw); err != nil {
		t.Fatal(err)
	}
	assertCanvasArrayJSON(t, draft)
	assertCanvasArrayJSON(t, raw)
}
