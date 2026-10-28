package proxy

import (
	"encoding/json"
	"testing"
)

func TestClientCanvasPublishCreatesNoNodeObjectsOrRevisions(t *testing.T) {
	p := newPublishPanel(t)
	// A client chain can use the same granted inbound twice; the clones have
	// distinct client names and no managed-node object is created for them.
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "a", Ref: "inbound:" + p.inbound.ID, Role: "entry"}, {InstanceID: "b", Ref: "inbound:" + p.inbound.ID, Role: "exit"}}, Connections: []CanvasConnection{{ID: "edge", Source: "a", Target: "b", Type: "proxy_chain", SupportTCP: true}}}
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "client fixture", Execution: "client", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]int{}
	for _, table := range []string{"config_revisions", "inbounds", "outbounds", "proxy_chains"} {
		var count int
		if err = p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		before[table] = count
	}
	result, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
	if err != nil || len(result.Nodes) != 0 || p.deployer.calls != 0 {
		t.Fatalf("client publish reached deployment: %v", err)
	}
	for table, count := range before {
		var after int
		if err = p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&after); err != nil || after != count {
			t.Fatalf("client publish mutated %s", table)
		}
	}
	var raw string
	if err = p.database.QueryRow("SELECT client_published_layout FROM canvas_schemes WHERE id=?", scheme.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var published CanvasLayout
	if json.Unmarshal([]byte(raw), &published) != nil || ValidateClientLayout(published) != nil {
		t.Fatal("snapshot missing")
	}
	layout.Connections = append(layout.Connections, CanvasConnection{ID: "cycle", Source: "b", Target: "a", Type: "proxy_chain"})
	if ValidateClientLayout(layout) == nil {
		t.Fatal("cycle accepted")
	}
}
