package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type renewalClient struct {
	pb.AgentControlClient
	authority *pki.Authority
	calls     int
	fail      bool
	key       []byte
	t         *testing.T
}

func (c *renewalClient) RenewCertificate(ctx context.Context, r *pb.CertificateRequest, _ ...grpc.CallOption) (*pb.CertificateRenewal, error) {
	c.calls++
	csr, err := pki.ParseCSR(r.CsrPem)
	if err != nil {
		c.t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(csr.PublicKey)
	if err != nil || !bytes.Equal(public, c.key) || r.RotateKey || r.Immediate {
		c.t.Fatal("renewal changed key or mode")
	}
	if c.fail {
		return nil, errors.New("offline")
	}
	cert, leaf, err := c.authority.IssueNode(csr, "renew-control")
	if err != nil {
		return nil, err
	}
	return &pb.CertificateRenewal{CertificatePem: cert, CaPem: c.authority.PEM, ExpiresAt: timestamppb.New(leaf.NotAfter)}, nil
}
func TestConnectedRenewalUsesSameKeyAndBacksOff(t *testing.T) {
	dir := t.TempDir()
	authority, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := identity.CSR(dir)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, leaf, err := authority.IssueNode(csr, "renew-control")
	if err != nil {
		t.Fatal(err)
	}
	leaf.NotBefore = time.Now().Add(-31 * 24 * time.Hour)
	leaf.NotAfter = time.Now().Add(59 * 24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, authority.Certificate, csr.PublicKey, authority.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.SaveEnrollment(dir, "renew-control", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), authority.PEM); err != nil {
		t.Fatal(err)
	}
	state, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := x509.MarshalPKIXPublicKey(csr.PublicKey)
	client := &renewalClient{authority: authority, fail: true, key: public, t: t}
	now := time.Now()
	check := renewalCheckAt(state, func() pb.AgentControlClient { return client }, func() time.Time { return now })
	original, _ := os.ReadFile(filepath.Join(dir, "client.key"))
	if ok, err := check(context.Background()); ok || err == nil || client.calls != 1 {
		t.Fatal(ok, err, client.calls)
	}
	now = now.Add(59 * time.Minute)
	if ok, err := check(context.Background()); ok || err != nil || client.calls != 1 {
		t.Fatal(ok, err, client.calls)
	}
	now = now.Add(time.Minute)
	client.fail = false
	if ok, err := check(context.Background()); !ok || err != nil || client.calls != 2 {
		t.Fatal(ok, err, client.calls)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "client.key"))
	if !bytes.Equal(original, after) {
		t.Fatal("private key changed")
	}
	if ok, err := check(context.Background()); ok || err != nil || client.calls != 2 {
		t.Fatal("fresh cert renewed again", ok, err, client.calls)
	}
}
