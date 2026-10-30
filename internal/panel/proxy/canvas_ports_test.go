package proxy

import "testing"

func TestCanvasImplicitPortRangeAndRepeatPublish(t *testing.T) {
	p := newPublishPanel(t)
	if _, err := p.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,20000,20002)", p.nodeB); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "reserved", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 20000, Enabled: false, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	c := canvasTestHop("i-a", "i-b", 0)
	c.TunnelPort = nil
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{c})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "range", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
			t.Fatal(err)
		}
		inbounds, _, _ := p.publishedObjects(t, scheme.Name)
		if len(inbounds) != 1 || inbounds[0].Port != 20001 {
			t.Fatal("range or repeat allocation", inbounds)
		}
	}
	if _, err := p.database.Exec("UPDATE node_meta SET port_range_start=20000,port_range_end=20000 WHERE node_id=?", p.nodeB); err != nil {
		t.Fatal(err)
	}
	if plan, err := p.service.canvasBuildPlan(p.ctx, scheme); err != nil || plan.edges[0].port != 20001 {
		t.Fatal("existing tunnel lost its stable port", plan, err)
	}
}

// The allocator works on numbers, not transports: a listener of either transport
// keeps it away from that port, so an implicit TCP relay never lands on a number a
// UDP listener already uses.
func TestCanvasPortRangeSkipsOccupiedNumbers(t *testing.T) {
	p := newPublishPanel(t)
	if _, err := p.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,20000,20001)", p.nodeB); err != nil {
		t.Fatal(err)
	}
	sealed, err := p.service.Repository.Vault.Seal([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	// Hysteria2 listens on UDP only while the relay the canvas allocates is a TCP
	// Trojan listener, so only the node-wide numeric rule keeps them apart.
	if _, err := p.database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('udp','udp',?,'hysteria2','0.0.0.0',20000,1,0,0,'direct','[]',?,'now')", p.nodeB, sealed); err != nil {
		t.Fatal(err)
	}
	c := canvasTestHop("i-a", "i-b", 0)
	c.TunnelPort = nil
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{c})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "numbers", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil || plan.edges[0].port != 20001 {
		t.Fatalf("allocator reused a UDP listener's number: %+v %v", plan, err)
	}
}
