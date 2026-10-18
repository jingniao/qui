package singbox

import (
	"context"
	"os"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestOfficialXrayClientNativeSingboxUDPInteroperability(t *testing.T) {
	peerBinary, clientBinary := os.Getenv("SINGBOX_BINARY"), os.Getenv("XRAY_BINARY")
	if peerBinary == "" || clientBinary == "" {
		t.Skip("set SINGBOX_BINARY and XRAY_BINARY for actual cross-core UDP")
	}
	for _, protocol := range []string{"vless", "trojan", "shadowsocks"} {
		t.Run(protocol, func(t *testing.T) {
			ctx := context.Background()
			peerState := stateFixture(t, protocol, freePort(t))
			peer := &Adapter{Path: peerBinary, Directory: t.TempDir(), NodeID: peerState.NodeID}
			defer peer.Close()
			rendered, err := peer.Render(ctx, peerState)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := peer.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			port := freePort(t)
			clientState := stateFixture(t, "socks5", port)
			clientState.NodeID = "xray-native-client"
			clientState.Credentials[0].Username = "udp-user"
			clientState.Credentials[0].Secret = "udp-auth-password"
			credential := peerState.Credentials[0]
			credential.ID = "upstream"
			credential.Internal = true
			credential.UserID = ""
			credential.DeviceID = ""
			clientState.Credentials = append(clientState.Credentials, credential)
			settings := peerState.Inbounds[0].Settings
			settings.TrustedCAPEM = settings.CertificatePEM
			settings.CertificatePEM = ""
			settings.PrivateKeyPEM = ""
			clientState.Outbounds = append(clientState.Outbounds, desiredstate.Outbound{ID: "peer", Protocol: protocol, Enabled: true, Address: "127.0.0.1", Port: peerState.Inbounds[0].Port, CredentialID: "upstream", Settings: settings})
			clientState.Inbounds[0].DefaultOutbound = "peer"
			client := &xray.Adapter{Path: clientBinary, Directory: t.TempDir(), NodeID: clientState.NodeID}
			defer client.Close()
			compiled, err := client.Render(ctx, clientState)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := client.Apply(ctx, compiled); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			udpEcho(t, port)
		})
	}
}

func TestNativeThreeProtocolChainUDPWithIndependentRelayKeys(t *testing.T) {
	peerBinary, clientBinary := os.Getenv("SINGBOX_BINARY"), os.Getenv("XRAY_BINARY")
	if peerBinary == "" || clientBinary == "" {
		t.Skip("set SINGBOX_BINARY and XRAY_BINARY for the three-hop UDP chain")
	}
	ctx := context.Background()
	exit := stateFixture(t, "shadowsocks", freePort(t))
	exit.NodeID = "exit"
	exit.Credentials[0].Internal = true
	exit.Credentials[0].UserID = ""
	exit.Credentials[0].DeviceID = ""
	exit.Credentials[0].ID = "exit-relay"
	exit.Inbounds[0].CredentialIDs = []string{"exit-relay"}
	base := exit.Credentials[0]
	base.ID = "exit-base"
	base.Secret = "unused-independent-base"
	exit.Credentials = append(exit.Credentials, base)
	exit.Inbounds[0].CredentialIDs = append(exit.Inbounds[0].CredentialIDs, base.ID)
	middle := stateFixture(t, "trojan", freePort(t))
	middle.NodeID = "middle"
	middle.Credentials[0].ID = "middle-relay"
	middle.Credentials[0].Internal = true
	middle.Credentials[0].UserID = ""
	middle.Credentials[0].DeviceID = ""
	middle.Inbounds[0].CredentialIDs = []string{"middle-relay"}
	middle.Credentials = append(middle.Credentials, exit.Credentials[0])
	middle.Inbounds[0].DefaultOutbound = "to-exit"
	middle.Outbounds = append(middle.Outbounds, desiredstate.Outbound{ID: "to-exit", Protocol: "shadowsocks", Enabled: true, Address: "127.0.0.1", Port: exit.Inbounds[0].Port, CredentialID: "exit-relay"})
	entry := stateFixture(t, "vless", freePort(t))
	entry.NodeID = "entry"
	entry.Credentials = append(entry.Credentials, middle.Credentials[0])
	settings := middle.Inbounds[0].Settings
	settings.TrustedCAPEM = settings.CertificatePEM
	settings.CertificatePEM = ""
	settings.PrivateKeyPEM = ""
	entry.Outbounds = append(entry.Outbounds, desiredstate.Outbound{ID: "to-middle", Protocol: "trojan", Enabled: true, Address: "127.0.0.1", Port: middle.Inbounds[0].Port, CredentialID: "middle-relay", Settings: settings})
	entry.Inbounds[0].DefaultOutbound = "block"
	entry.ProxyChains = []desiredstate.ProxyChain{{ID: "path", NodeIDs: []string{"entry", "middle", "exit"}, OutboundIDs: []string{"to-middle", "to-exit"}, Failover: "Block"}}
	entry.Policies = []desiredstate.Policy{{ID: "chain-services", Enabled: true, Match: desiredstate.Match{CIDRs: []string{"127.0.0.1/32"}, Network: "all"}, Action: desiredstate.Action{Type: "ProxyChain", TargetID: "path"}}}
	for _, state := range []desiredstate.State{exit, middle, entry} {
		a := &Adapter{Path: peerBinary, Directory: t.TempDir(), NodeID: state.NodeID}
		t.Cleanup(func() { a.Close() })
		compiled, err := a.Render(ctx, state)
		if err != nil {
			t.Fatal(state.NodeID, err)
		}
		if result, err := a.Apply(ctx, compiled); err != nil || !result.Success {
			t.Fatal(state.NodeID, result, err)
		}
	}
	port := freePort(t)
	clientState := stateFixture(t, "socks5", port)
	clientState.NodeID = "client"
	clientState.Credentials[0].Username = "udp-user"
	clientState.Credentials[0].Secret = "udp-auth-password"
	upstream := entry.Credentials[0]
	upstream.ID = "upstream"
	upstream.Internal = true
	upstream.UserID = ""
	upstream.DeviceID = ""
	clientState.Credentials = append(clientState.Credentials, upstream)
	clientState.Outbounds = append(clientState.Outbounds, desiredstate.Outbound{ID: "remote-entry", Protocol: "vless", Enabled: true, Address: "127.0.0.1", Port: entry.Inbounds[0].Port, CredentialID: "upstream"})
	clientState.Inbounds[0].DefaultOutbound = "remote-entry"
	clientState.Credentials = []desiredstate.Credential{upstream}
	clientState.Inbounds[0].CredentialIDs = nil
	client := &xray.Adapter{Path: clientBinary, Directory: t.TempDir(), NodeID: clientState.NodeID}
	defer client.Close()
	compiled, err := client.Render(ctx, clientState)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := client.Apply(ctx, compiled); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	udpEcho(t, port, false)
}
