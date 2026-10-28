package singbox

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// A9.2 runs in both cores CI steps: the official build proves unknown, and the
// qui build compares both users across all four protocols to payload and Xray.
func TestRealUserStatsTwoUsersProtocolPayloadAndXrayParity(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("awaiting CI/Owner run: set SINGBOX_BINARY and XRAY_BINARY")
	}
	ctx := context.Background()
	caps, err := Detect(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.SingboxUserStats {
		a := &Adapter{Path: binary, Directory: t.TempDir()}
		if _, err := a.DetectCapabilities(ctx); err != nil {
			t.Fatal(err)
		}
		if a.SupportsAccountTelemetry() {
			t.Fatal("official binary claims user stats")
		}
		if _, err := a.TrafficCounters(ctx); err != ErrTelemetryUnavailable {
			t.Fatal("official traffic must remain unknown", err)
		}
		return
	}
	xrayBinary := os.Getenv("XRAY_BINARY")
	if xrayBinary == "" {
		t.Skip("awaiting CI/Owner run: set XRAY_BINARY for counter parity")
	}
	for _, protocol := range []string{"vless", "trojan", "hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			serverState := stateFixture(t, protocol, freePort(t))
			if protocol == "hysteria2" || protocol == "tuic" {
				packet, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				serverState.Inbounds[0].Port = packet.LocalAddr().(*net.UDPAddr).Port
				packet.Close()
				serverState.Inbounds[0].Settings = localTLS(t)
				serverState.Inbounds[0].Settings.Transport = "quic"
				serverState.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{UpMbps: 100, DownMbps: 100}
				if protocol == "tuic" {
					serverState.Credentials[0].Username = "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"
					serverState.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{CongestionControl: "bbr"}
				}
			}
			second := serverState.Credentials[0]
			second.ID = "credential-2"
			second.UserID = "user-2"
			second.DeviceID = "identity-2"
			if protocol == "vless" {
				second.Secret = "7ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"
			} else {
				second.Secret = "second-user-password"
			}
			if protocol == "tuic" {
				second.Username = "7ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"
			}
			serverState.Users = append(serverState.Users, desiredstate.User{ID: second.UserID, Enabled: true})
			serverState.Credentials = append(serverState.Credentials, second)
			serverState.Inbounds[0].CredentialIDs = append(serverState.Inbounds[0].CredentialIDs, second.ID)
			server := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: serverState.NodeID}
			server.ConfigureRuntime(core.RuntimeHooks{EnableStats: true})
			defer server.Close()
			rendered, err := server.Render(ctx, serverState)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := server.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			referenceState := stateFixture(t, "socks5", freePort(t))
			referenceState.NodeID = "xray-reference"
			referenceSecond := referenceState.Credentials[0]
			referenceSecond.ID = "reference-2"
			referenceSecond.UserID = "user-2"
			referenceSecond.DeviceID = "identity-2"
			referenceSecond.Username = "reference-user-2"
			referenceState.Users = append(referenceState.Users, desiredstate.User{ID: "user-2", Enabled: true})
			referenceState.Credentials = append(referenceState.Credentials, referenceSecond)
			referenceState.Inbounds[0].CredentialIDs = append(referenceState.Inbounds[0].CredentialIDs, referenceSecond.ID)
			reference := &xray.Adapter{Path: xrayBinary, Directory: t.TempDir(), NodeID: referenceState.NodeID, EnableStats: true}
			defer reference.Close()
			xconfig, err := reference.Render(ctx, referenceState)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := reference.Apply(ctx, xconfig); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			for i, credential := range serverState.Credentials {
				payload := strings.Repeat("x", (i+1)*256*1024)
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != payload {
						t.Error("origin payload mismatch", len(body), err)
					}
					w.Header().Set("Date", "Tue, 27 Oct 2026 12:00:00 GMT")
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					io.WriteString(w, payload)
				}))
				defer origin.Close()
				clientState := stateFixture(t, "socks5", freePort(t))
				clientState.NodeID = "client-" + credential.ID
				upstream := credential
				upstream.ID = "upstream"
				upstream.Internal = true
				upstream.UserID = ""
				upstream.DeviceID = ""
				clientState.Credentials = append(clientState.Credentials, upstream)
				settings := serverState.Inbounds[0].Settings
				settings.TrustedCAPEM = settings.CertificatePEM
				settings.CertificatePEM = ""
				settings.PrivateKeyPEM = ""
				clientState.Outbounds = append(clientState.Outbounds, desiredstate.Outbound{ID: "to-server", Protocol: protocol, Enabled: true, Address: "127.0.0.1", Port: serverState.Inbounds[0].Port, CredentialID: upstream.ID, Settings: settings})
				clientState.Inbounds[0].DefaultOutbound = "to-server"
				client := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: clientState.NodeID}
				defer client.Close()
				config, err := client.Render(ctx, clientState)
				if err != nil {
					t.Fatal(err)
				}
				if result, err := client.Apply(ctx, config); err != nil || !result.Success {
					t.Fatal(result, err)
				}
				for _, path := range []struct {
					port               int
					username, password string
				}{{clientState.Inbounds[0].Port, clientState.Credentials[0].Username, clientState.Credentials[0].Secret}, {referenceState.Inbounds[0].Port, referenceState.Credentials[i].Username, referenceState.Credentials[i].Secret}} {
					proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(path.port))
					proxyURL.User = url.UserPassword(path.username, path.password)
					transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
					response, err := (&http.Client{Transport: transport, Timeout: 10 * time.Second}).Post(origin.URL, "application/octet-stream", strings.NewReader(payload))
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(response.Body)
					response.Body.Close()
					transport.CloseIdleConnections()
					if err != nil || string(body) != payload {
						t.Fatal("client payload mismatch", len(body), err)
					}
				}
			}
			var actual, expected []traffic.Counter
			deadline := time.Now().Add(5 * time.Second)
			for {
				actual, err = server.TrafficCounters(ctx)
				if err != nil {
					t.Fatal(err)
				}
				expected, err = reference.TrafficCounters(ctx)
				if err != nil {
					t.Fatal(err)
				}
				complete := len(actual) == 2 && len(expected) == 2
				for _, c := range actual {
					payload := int64(256 * 1024)
					if c.UserID == "user-2" {
						payload *= 2
					}
					matched := false
					for _, reference := range expected {
						if reference.UserID == c.UserID && reference.UploadBytes == c.UploadBytes && reference.DownloadBytes == c.DownloadBytes {
							matched = true
						}
					}
					complete = complete && matched && c.UploadBytes >= payload && c.DownloadBytes >= payload
				}
				if complete {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("missing/incomplete counters or Xray parity mismatch", actual, expected)
				}
				time.Sleep(50 * time.Millisecond)
			}
			byUser := map[string]traffic.Counter{}
			for _, c := range expected {
				byUser[c.UserID] = c
			}
			for i, c := range actual {
				payload := int64((i + 1) * 256 * 1024)
				for _, value := range []int64{c.UploadBytes, c.DownloadBytes} {
					if value < payload*95/100 || value > payload*105/100 {
						t.Fatal("outside payload tolerance", c, payload)
					}
				}
				r := byUser[c.UserID]
				if c.UploadBytes != r.UploadBytes || c.DownloadBytes != r.DownloadBytes {
					t.Fatal("Xray/sing-box byte parity mismatch", c, r)
				}
			}
			again, err := server.TrafficCounters(ctx)
			if err != nil || len(again) != 2 {
				t.Fatal(again, err)
			}
			for i, c := range again {
				if c.UploadBytes != actual[i].UploadBytes || c.DownloadBytes != actual[i].DownloadBytes || c.CoreEpoch != actual[i].CoreEpoch {
					t.Fatal("read reset counters")
				}
			}
		})
	}
}
