package proxy

import (
	"encoding/json"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestCanvasEntryPlanManualPortReservedBeforeAutoGostRelay(t *testing.T) {
	p, scheme, _ := gostRelayPanel(t)
	n, err := p.service.Nodes.Get(p.ctx, p.nodeB)
	if err != nil {
		t.Fatal(err)
	}
	n.Capabilities.Cores = append(n.Capabilities.Cores, &pb.CoreCapability{Name: n.Capabilities.CoreName, Protocols: n.Capabilities.Protocols})
	raw, err := json.Marshal(n.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), p.nodeB); err != nil {
		t.Fatal(err)
	}
	layout := scheme.Layout
	layout.Instances[1].Role = "entry"
	layout.Instances[1].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(23000), GroupMode: "none"}
	layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "i-c", Ref: "server:" + p.nodeC, Role: "exit"})
	layout.Connections = append(layout.Connections, canvasTestHop("i-b", "i-c", 9443))
	result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &layout)
	if err != nil || !result.OK {
		t.Fatalf("manual entry conflicts with automatic GOST relay: %+v, %v", result, err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Port != 23000 {
		t.Fatalf("manual entry changed: %+v", result.Entries)
	}
	for _, server := range result.Servers {
		if server.NodeID != p.nodeB {
			continue
		}
		for _, object := range server.Creates {
			if object.Kind == "forward" && object.Port == 23001 {
				return
			}
		}
	}
	t.Fatalf("GOST relay did not use the next free port: %+v", result.Servers)
}
