package proxy

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// selfSignedInbound is the Owner's side of a panel-issued certificate: a TLS
// listener whose source is self_signed, so the panel generates the material.
func selfSignedInbound(nodeID string, port int) InboundInput {
	return InboundInput{Name: "vless entry", NodeID: nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: port, Enabled: true, DefaultOutbound: "direct", TLSSource: TLSSourceSelfSigned, Settings: desiredstate.ProtocolSettings{Security: "tls"}}
}

func revealInbound(t *testing.T, f *fixture, id string) Inbound {
	t.Helper()
	inbound, err := f.service.Repository.Inbound(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	return inbound
}

func certificateOf(t *testing.T, certificatePEM string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil {
		t.Fatal("the stored certificate is not PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

// recordCertificateFiles reports one core to the panel, with or without the node
// certificate file renderer.
func recordCertificateFiles(t *testing.T, f *fixture, nodeID, protocol string, supported bool) {
	t.Helper()
	capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{
		Name: "xray", Version: "26.3.27", TlsCertificateFilesSupported: supported,
		Protocols: []*pb.ProtocolCapability{{Protocol: protocol, Inbound: true, Outbound: true, Tcp: true, Udp: true}},
	}}}
	if _, err := f.nodes.Record(context.Background(), nodeID, &pb.AgentMessage{NodeId: nodeID, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}); err != nil {
		t.Fatal(err)
	}
}

// A generated certificate has to serve the address clients dial, which is the
// node's connect host when the Owner set one and its public address otherwise.
func TestSelfSignedCertificateCoversTheNodeConnectAddress(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first, err := f.service.SaveInbound(ctx, "", selfSignedInbound(f.nodeA, 24443))
	if err != nil {
		t.Fatalf("the panel did not issue a certificate: %v", err)
	}
	stored := revealInbound(t, f, first.ID)
	if stored.TLSSource != TLSSourceSelfSigned || stored.Settings.ServerName != "192.0.2.10" || certificateOf(t, stored.Settings.CertificatePEM).VerifyHostname("192.0.2.10") != nil {
		t.Fatalf("the certificate does not serve the node's public address: %s", stored.Settings.ServerName)
	}
	if stored.Settings.TrustedCAPEM != stored.Settings.CertificatePEM {
		t.Fatal("the pinned CA is not the presented certificate")
	}
	if _, err := f.service.Repository.Database.Exec("UPDATE nodes SET connect_host=? WHERE id=?", "edge.example.com", f.nodeB); err != nil {
		t.Fatal(err)
	}
	second, err := f.service.SaveInbound(ctx, "", selfSignedInbound(f.nodeB, 24443))
	if err != nil {
		t.Fatal(err)
	}
	stored = revealInbound(t, f, second.ID)
	if stored.Settings.ServerName != "edge.example.com" || certificateOf(t, stored.Settings.CertificatePEM).VerifyHostname("edge.example.com") != nil {
		t.Fatalf("the certificate does not serve the connect host: %s", stored.Settings.ServerName)
	}
}

// There is nothing to name in a certificate when the node has no address at all.
func TestSelfSignedCertificateNeedsANodeAddress(t *testing.T) {
	f := setup(t)
	third, _, _, err := f.nodes.CreateEnrollment(context.Background(), "Node C", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveInbound(context.Background(), "", selfSignedInbound(third.ID, 24443)); err == nil || !strings.Contains(err.Error(), "请先在节点设置里填写公网 IP 或客户端连接地址") {
		t.Fatalf("a node without an address issued a certificate: %v", err)
	}
}

// An ordinary save keeps the certificate the clients already pinned; only an
// explicit regenerate issues a new one.
func TestSelfSignedCertificateIsKeptOnAPlainEditAndRotatedOnRequest(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	created, err := f.service.SaveInbound(ctx, "", selfSignedInbound(f.nodeA, 24443))
	if err != nil {
		t.Fatal(err)
	}
	before := revealInbound(t, f, created.ID)
	if _, err := f.service.SaveInbound(ctx, created.ID, selfSignedInbound(f.nodeA, 24443)); err != nil {
		t.Fatalf("a plain edit was refused: %v", err)
	}
	kept := revealInbound(t, f, created.ID)
	if kept.Settings.CertificatePEM != before.Settings.CertificatePEM || kept.Settings.PrivateKeyPEM != before.Settings.PrivateKeyPEM {
		t.Fatal("a plain edit replaced the pinned certificate")
	}
	rotate := selfSignedInbound(f.nodeA, 24443)
	rotate.RegenerateCertificate = true
	if _, err := f.service.SaveInbound(ctx, created.ID, rotate); err != nil {
		t.Fatalf("regenerate was refused: %v", err)
	}
	if rotated := revealInbound(t, f, created.ID); rotated.Settings.CertificatePEM == before.Settings.CertificatePEM {
		t.Fatal("regenerate kept the old certificate")
	}
}

// Switching the source to self_signed must issue a certificate of its own: the
// stored pair is merged into the request before the branch runs, so a relabelled
// pasted certificate would otherwise pass for a generated one.
func TestSwitchingToSelfSignedReplacesThePastedCertificate(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pasted, err := managedTLS("pasted.example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	input := selfSignedInbound(f.nodeA, 24443)
	input.TLSSource, input.Settings = TLSSourcePaste, pasted
	created, err := f.service.SaveInbound(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if created.TLSSource != TLSSourcePaste {
		t.Fatalf("the pasted source was not recorded: %q", created.TLSSource)
	}
	switched, err := f.service.SaveInbound(ctx, created.ID, selfSignedInbound(f.nodeA, 24443))
	if err != nil {
		t.Fatalf("switching to self_signed was refused: %v", err)
	}
	stored := revealInbound(t, f, switched.ID)
	if stored.TLSSource != TLSSourceSelfSigned || stored.Settings.ServerName != "192.0.2.10" || stored.Settings.CertificatePEM == pasted.CertificatePEM {
		t.Fatal("switching to self_signed only relabelled the pasted certificate")
	}
}

// The panel describes the certificate it holds so the editor can show what it
// covers and when it expires, and warns when a client would reject it.
func TestCertificateInfoReportsNamesExpiryAndCoverage(t *testing.T) {
	settings, err := managedTLS("edge.example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	info, err := certificateInfo(settings, TLSSourceSelfSigned, "edge.example.com")
	if err != nil || info == nil {
		t.Fatalf("a stored certificate was not described: %v", err)
	}
	if len(info.DNSNames) != 1 || info.DNSNames[0] != "edge.example.com" || info.NotAfter == "" || !info.SelfSigned || info.CoverageWarning != "" {
		t.Fatalf("the certificate details are incomplete: %+v", info)
	}
	// The dialled address alone is enough to be covered.
	bare := settings
	bare.ServerName = ""
	if info, err = certificateInfo(bare, TLSSourceSelfSigned, "edge.example.com"); err != nil || info == nil || info.CoverageWarning != "" {
		t.Fatalf("a covered address warned: %v %+v", err, info)
	}
	mismatched := settings
	mismatched.ServerName = "mismatch.example.com"
	if info, err = certificateInfo(mismatched, TLSSourceSelfSigned, "198.51.100.7"); err != nil || info == nil {
		t.Fatalf("a mismatched certificate was not described: %v", err)
	}
	if !strings.Contains(info.CoverageWarning, "mismatch.example.com") || !strings.Contains(info.CoverageWarning, "198.51.100.7") {
		t.Fatalf("the coverage warning is incomplete: %q", info.CoverageWarning)
	}
	// A node-side file is unreadable here, so there is nothing to describe.
	if info, err := certificateInfo(desiredstate.ProtocolSettings{CertificatePath: desiredstate.CertificateDirectory + "/node.crt"}, TLSSourceFile, ""); err != nil || info != nil {
		t.Fatalf("a node-side file was described: %v %+v", err, info)
	}
}

// A node certificate file is stored only for a core that reported the renderer
// that reads it, and the panel never holds the private key.
func TestFileCertificateSourceNeedsTheCoreReport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	recordCertificateFiles(t, f, f.nodeA, "vless", false)
	recordCertificateFiles(t, f, f.nodeB, "vless", true)
	files := func(nodeID string) InboundInput {
		input := selfSignedInbound(nodeID, 24443)
		input.TLSSource = TLSSourceFile
		input.Settings = desiredstate.ProtocolSettings{Security: "tls", CertificatePath: desiredstate.CertificateDirectory + "/node.crt", PrivateKeyPath: desiredstate.CertificateDirectory + "/node.key"}
		return input
	}
	if _, err := f.service.SaveInbound(ctx, "", files(f.nodeA)); err == nil || !strings.Contains(err.Error(), "certificate file") {
		t.Fatalf("a core without the report stored certificate files: %v", err)
	}
	saved, err := f.service.SaveInbound(ctx, "", files(f.nodeB))
	if err != nil {
		t.Fatalf("a declared certificate file was refused: %v", err)
	}
	if saved.TLSSource != TLSSourceFile || saved.Settings.CertificatePath != desiredstate.CertificateDirectory+"/node.crt" {
		t.Fatalf("the file source was not stored: %+v", saved)
	}
	if saved.HasTLSKey {
		t.Fatal("a node-side file reported a stored private key")
	}
}

// An internal relay presents the material the panel writes for it and copies its
// settings to another node, so a node-side pair is never meaningful there.
func TestInternalRelayRefusesNodeCertificateFiles(t *testing.T) {
	f := setup(t)
	recordCertificateFiles(t, f, f.nodeA, "trojan", true)
	input := InboundInput{Name: "relay", NodeID: f.nodeA, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, DefaultOutbound: "direct", TLSSource: TLSSourceFile,
		Settings: desiredstate.ProtocolSettings{Security: "tls", CertificatePath: desiredstate.CertificateDirectory + "/relay.crt", PrivateKeyPath: desiredstate.CertificateDirectory + "/relay.key"}}
	if _, err := f.service.SaveInbound(context.Background(), "", input); err == nil || !strings.Contains(err.Error(), "内部中转入站使用自己的证书材料") {
		t.Fatalf("an internal relay accepted node certificate files: %v", err)
	}
}
