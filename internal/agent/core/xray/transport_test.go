package xray

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
)

func TestRealVlessWebSocketAndGRPCTransport(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for transport integration")
	}
	for _, transport := range []string{"ws", "grpc"} {
		t.Run(transport, func(t *testing.T) {
			credential := proxyCredentials(t, "vless")
			settings := tlsMaterials(t)
			settings.Transport = transport
			settings.Path = "/proxy-control"
			settings.ServiceName = "proxy-control"
			entryPort := availablePort(t)
			server := testState(entryPort, 1)
			server.Credentials = []desiredstate.Credential{credential}
			server.Inbounds[0].Protocol = "vless"
			server.Inbounds[0].CredentialIDs = []string{credential.ID}
			server.Inbounds[0].Settings = settings
			startState(t, server)
			localPort := availablePort(t)
			client := testState(localPort, 1)
			client.Credentials = []desiredstate.Credential{credential}
			client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "entry", Protocol: "vless", Address: "127.0.0.1", Port: entryPort, Enabled: true, CredentialID: credential.ID, Settings: clientTLS(settings)})
			client.Inbounds[0].DefaultOutbound = "entry"
			startState(t, client)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "transport-verified") }))
			defer origin.Close()
			if !strings.Contains(socksRequest(t, localPort, origin.URL), "transport-verified") {
				t.Fatal("transport failed")
			}
		})
	}
}
