package proxy

import (
	"context"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestInternalQUICRelayGeneratesTLSAndSeparateBoundCredentials(t *testing.T) {
	for _, protocol := range []string{"hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			target, err := f.service.SaveInbound(ctx, "", InboundInput{Name: "QUIC relay", NodeID: f.nodeB, Protocol: protocol, Listen: "0.0.0.0", Port: 9443, Enabled: true, InternalRelay: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Transport: "quic", QUIC: &desiredstate.QUICSettings{}}})
			if err != nil {
				t.Fatal(err)
			}
			one, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "QUIC one", NodeID: f.nodeA, Protocol: protocol, Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true})
			if err != nil {
				t.Fatal(err)
			}
			two, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "QUIC two", NodeID: f.nodeA, Protocol: protocol, Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true})
			if err != nil {
				t.Fatal(err)
			}
			first, err := f.service.Repository.Credential(ctx, one.CredentialID)
			if err != nil {
				t.Fatal(err)
			}
			second, err := f.service.Repository.Credential(ctx, two.CredentialID)
			if err != nil {
				t.Fatal(err)
			}
			if first.Secret == second.Secret || first.ID == second.ID || !first.Internal {
				t.Fatal("QUIC relay reused identity")
			}
			if protocol == "tuic" {
				if err := desiredstate.ValidateUUID(first.Username); err != nil {
					t.Fatal(err)
				}
			}
			remote, err := f.service.Snapshot(ctx, f.nodeB, f.users)
			if err != nil {
				t.Fatal(err)
			}
			if err := remote.Validate(); err != nil {
				t.Fatal(err)
			}
			for _, entry := range remote.Inbounds {
				if entry.SubscriptionVisible {
					t.Fatal("internal QUIC appeared in subscriptions")
				}
			}
			source, err := f.service.Snapshot(ctx, f.nodeA, f.users)
			if err != nil {
				t.Fatal(err)
			}
			if err := source.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
