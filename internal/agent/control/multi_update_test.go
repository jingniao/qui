package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/release"
)

func TestMultiComponentUpdateKeepsPeerRunning(t *testing.T) {
	a := &fakeCoreRuntime{version: "26.3.27"}
	w, c, m, original := updateHarness(t, a)
	b := &fakeCoreRuntime{name: "sing-box", version: "1.12.0", selected: original}
	multi, err := core.NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	m.Adapter = multi
	s := desiredstate.State{SchemaVersion: 1, Revision: 2, NodeID: "node-update", Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}, Inbounds: []desiredstate.Inbound{{ID: "a", Protocol: "socks5", Listen: "127.0.0.1", Port: 10101, Enabled: true, DefaultOutbound: "direct"}, {ID: "b", Core: "sing-box", Protocol: "socks5", Listen: "127.0.0.1", Port: 10102, Enabled: true, DefaultOutbound: "direct"}}}
	if _, err := m.Apply(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	peerApplies := len(b.applied)
	archive, digest, _ := updateArchive(t, original, "multi-update")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) }))
	defer server.Close()
	w.source = updateSource(t, server)
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_multi", NodeID: "node-update", Revision: 2, Sequence: 1, Component: "xray", Version: "26.3.27", URL: updateAssetURL, SHA256: digest}
	command := updateRequest(request)
	command.Component = "xray"
	if err := w.submit(command); err != nil {
		t.Fatal(err)
	}
	if result := awaitResult(t, w); result.Status != "success" {
		t.Fatalf("component update: %+v", result)
	}
	if len(b.applied) != peerApplies || b.activate != 0 {
		t.Fatal("healthy peer restarted")
	}
	if a.selected == original || m.CoreComponentHeld("xray") {
		t.Fatal("updated core not committed/released")
	}
	if _, err := os.Stat(filepath.Join(c.DataDirectory, "core-update-xray-hold.enc")); !os.IsNotExist(err) {
		t.Fatal("component hold survived a confirmed commit", err)
	}
}

func TestCoreUpdateEnvelopeComponentMustMatchPayload(t *testing.T) {
	a := &fakeCoreRuntime{version: "26.3.27"}
	w, _, _, _ := updateHarness(t, a)
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_scope", NodeID: "node-update", Revision: 1, Sequence: 1, Component: "xray", Version: "26.3.27", URL: updateAssetURL, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	command := updateRequest(request)
	command.Component = "sing-box"
	if err := w.submit(command); err == nil {
		t.Fatal("ambiguous update component accepted")
	}
}
