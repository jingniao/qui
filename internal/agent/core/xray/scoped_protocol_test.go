package xray

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestRealScopedPoliciesAcrossAuthenticatedProtocols(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for protocol account policy tests")
	}
	for _, protocol := range []string{"http", "vless", "trojan", "shadowsocks"} {
		t.Run(protocol, func(t *testing.T) {
			first := proxyCredentials(t, protocol)
			first.ID = "first-device"
			first.UserID = "user"
			first.DeviceID = "first"
			first.Internal = false
			first.Username = "first"
			second := proxyCredentials(t, protocol)
			second.ID = "second-device"
			second.UserID = "user"
			second.DeviceID = "second"
			second.Internal = false
			second.Username = "second"
			entryPort := availablePort(t)
			server := testState(entryPort, 1)
			server.Users = []desiredstate.User{{ID: "user", Enabled: true}}
			server.Credentials = []desiredstate.Credential{first, second}
			server.Inbounds[0].Protocol = protocol
			server.Inbounds[0].CredentialIDs = []string{first.ID, second.ID}
			settings := desiredstate.ProtocolSettings{}
			if protocol == "trojan" {
				settings = tlsMaterials(t)
				server.Inbounds[0].Settings = settings
			}
			server.Policies = []desiredstate.Policy{{ID: "block-first", Priority: 1, Enabled: true, Scope: desiredstate.Scope{UserIDs: []string{"user"}, DeviceIDs: []string{"first"}}, Match: desiredstate.Match{Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}}}
			startState(t, server)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "scoped-policy-origin") }))
			defer origin.Close()
			for index, credential := range []desiredstate.Credential{first, second} {
				localPort := availablePort(t)
				client := testState(localPort, 1)
				client.Credentials = []desiredstate.Credential{credential}
				client.Users = []desiredstate.User{{ID: "user", Enabled: true}}
				client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "remote", Protocol: protocol, Address: "127.0.0.1", Port: entryPort, Enabled: true, CredentialID: credential.ID, Settings: clientTLS(settings)})
				client.Inbounds[0].DefaultOutbound = "remote"
				startState(t, client)
				_, allowed := domainRequest(t, localPort, "127.0.0.1", origin.Listener.Addr().(*net.TCPAddr).Port)
				if allowed != (index == 1) {
					t.Fatalf("%s selected device filtering wrong for %d", protocol, index)
				}
			}
			if _, err := Render(context.Background(), server); err != nil {
				t.Fatal(err)
			}
		})
	}
}
