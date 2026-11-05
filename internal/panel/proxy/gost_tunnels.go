package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"time"
)

// ErrGostTunnelMaterial reports a GOST tunnel whose credential set is missing or
// unreadable. The next publication would deploy an exit node that no entry can
// authenticate against, so it is refused instead of rendered.
var ErrGostTunnelMaterial = errors.New("gost tunnel credentials unavailable")

// gostMaterialLifetime is the validity of the CA and both leaves. G3 keeps one
// credential set per tunnel and no staged rotation, so a ten-year lifetime keeps
// the manual rotation rare; "rotate credentials" replaces the whole set.
const gostMaterialLifetime = 10 * 365 * 24 * time.Hour

// gostMaterial is the sealed credential set of one GOST tunnel: one CA, one
// server leaf for the exit relay listener and one client leaf for the entry
// forward. The CA private key is not kept after issuing, because nothing else is
// ever signed with it.
type gostMaterial struct {
	Next          *gostMaterial `json:"next,omitempty"`
	Generation    int64         `json:"generation"`
	ServerName    string        `json:"server_name"`
	CACertPEM     string        `json:"ca_cert_pem"`
	ServerCertPEM string        `json:"server_cert_pem"`
	ServerKeyPEM  string        `json:"server_key_pem"`
	ClientCertPEM string        `json:"client_cert_pem"`
	ClientKeyPEM  string        `json:"client_key_pem"`
	NotAfter      string        `json:"not_after"`
	Fingerprint   string        `json:"fingerprint"`
}

// New managed tunnels seal both legs at save/rotation time. Each leg has
// separate leaves and CA; planning never issues or changes credentials.
func newManagedGostMaterial(tunnelID string, generation int64) (gostMaterial, error) {
	m, err := newGostMaterial(tunnelID, generation)
	if err != nil {
		return m, err
	}
	next, err := newGostMaterial(tunnelID+":leg2", generation)
	m.Next = &next
	return m, err
}

// gostServerName is the synthetic DNS name the exit relay certificate carries and
// the entry dialer verifies. It is derived from the tunnel ID alone, so a
// rotation keeps the name and no node needs DNS for it.
func gostServerName(tunnelID string) string {
	digest := sha256.Sum256([]byte("gost-tunnel:" + tunnelID))
	return "t-" + hex.EncodeToString(digest[:8]) + ".gost.qui.internal"
}

// newGostMaterial issues one CA and the two leaves of a tunnel.
func newGostMaterial(tunnelID string, generation int64) (gostMaterial, error) {
	now := time.Now().UTC()
	notAfter := now.Add(gostMaterialLifetime)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return gostMaterial{}, err
	}
	ca := &x509.Certificate{
		SerialNumber:          serial(tunnelID, generation, "ca"),
		Subject:               pkix.Name{CommonName: "qui gost tunnel " + tunnelID},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return gostMaterial{}, err
	}
	serverName := gostServerName(tunnelID)
	server, err := gostLeaf(ca, caKey, tunnelID, generation, "server", &serverName, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return gostMaterial{}, err
	}
	client, err := gostLeaf(ca, caKey, tunnelID, generation, "client", nil, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return gostMaterial{}, err
	}
	fingerprint := sha256.Sum256(server.der)
	return gostMaterial{
		Generation:    generation,
		ServerName:    serverName,
		CACertPEM:     encodePEM("CERTIFICATE", caDER),
		ServerCertPEM: encodePEM("CERTIFICATE", server.der),
		ServerKeyPEM:  encodePEM("PRIVATE KEY", server.key),
		ClientCertPEM: encodePEM("CERTIFICATE", client.der),
		ClientKeyPEM:  encodePEM("PRIVATE KEY", client.key),
		NotAfter:      server.certificate.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint:   hex.EncodeToString(fingerprint[:]),
	}, nil
}

type issuedLeaf struct {
	der         []byte
	key         []byte
	certificate *x509.Certificate
}

// gostLeaf issues one leaf of the tunnel CA. The server leaf carries the synthetic
// DNS name and the server EKU, the client leaf the client EKU; both are ECDSA
// P-256 like the CA, so GOST and Go agree on the TLS suites.
func gostLeaf(ca *x509.Certificate, caKey *ecdsa.PrivateKey, tunnelID string, generation int64, role string, serverName *string, usage x509.ExtKeyUsage) (issuedLeaf, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return issuedLeaf{}, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial(tunnelID, generation, role),
		Subject:               pkix.Name{CommonName: "qui gost " + role + " " + tunnelID},
		NotBefore:             ca.NotBefore,
		NotAfter:              ca.NotAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{usage},
		BasicConstraintsValid: true,
	}
	if serverName != nil {
		template.DNSNames = []string{*serverName}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return issuedLeaf{}, err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return issuedLeaf{}, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return issuedLeaf{}, err
	}
	return issuedLeaf{der: der, key: private, certificate: certificate}, nil
}

// serial is deterministic per (tunnel, generation, role) so a material set is
// reproducible from its own fields, and unique enough that no two leaves of the
// deployment share one.
func serial(tunnelID string, generation int64, role string) *big.Int {
	digest := sha256.Sum256([]byte(tunnelID + "|" + role + "|" + time.Duration(generation).String()))
	return new(big.Int).SetBytes(digest[:20])
}

func encodePEM(kind string, data []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data}))
}

// valid checks one sealed set before it is used to render a node: the leaves must
// still verify against their own CA and carry the EKUs and name the two halves
// need. A set that fails here is refused rather than deployed.
func (m gostMaterial) valid(tunnelID string) error {
	if m.Next != nil {
		if err := m.Next.valid(tunnelID + ":leg2"); err != nil {
			return err
		}
	}
	if m.Generation < 1 || m.ServerName != gostServerName(tunnelID) {
		return ErrGostTunnelMaterial
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(m.CACertPEM)) {
		return ErrGostTunnelMaterial
	}
	server, err := x509.ParseCertificate(pemBlock(m.ServerCertPEM))
	if err != nil {
		return ErrGostTunnelMaterial
	}
	client, err := x509.ParseCertificate(pemBlock(m.ClientCertPEM))
	if err != nil {
		return ErrGostTunnelMaterial
	}
	for _, leaf := range []struct {
		certificate *x509.Certificate
		usage       x509.ExtKeyUsage
	}{{server, x509.ExtKeyUsageServerAuth}, {client, x509.ExtKeyUsageClientAuth}} {
		if _, err := leaf.certificate.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{leaf.usage}}); err != nil {
			return ErrGostTunnelMaterial
		}
	}
	if len(server.DNSNames) != 1 || server.DNSNames[0] != m.ServerName {
		return ErrGostTunnelMaterial
	}
	return nil
}

func pemBlock(data string) []byte {
	block, _ := pem.Decode([]byte(data))
	if block == nil {
		return nil
	}
	return block.Bytes
}

// tunnelDatabase is the database shape the material helpers need, so a read or a
// write works inside a transaction as well as on the pool.
type tunnelDatabase interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// gostRelay reports whether the tunnel is the GOST relay kind: a port_forward
// connection whose end-to-end hop is a GOST relay on TLS with a client CA.
func (t CanvasTunnel) gostRelay() bool {
	return t.Type == "port_forward" && t.TunnelProtocol == "gost"
}

// loadGostMaterial reads and validates the sealed material of one tunnel. The
// sealed bytes are returned too, so a caller can keep the column unchanged.
func (r *Repository) loadGostMaterial(ctx context.Context, q tunnelDatabase, tunnelID string) (gostMaterial, error) {
	var sealed []byte
	err := q.QueryRowContext(ctx, "SELECT gost_material FROM canvas_tunnels WHERE id=?", tunnelID).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return gostMaterial{}, ErrNotFound
	}
	if err != nil {
		return gostMaterial{}, err
	}
	return r.openGostMaterial(tunnelID, sealed)
}

func (r *Repository) openGostMaterial(tunnelID string, sealed []byte) (gostMaterial, error) {
	if len(sealed) == 0 || r.Vault == nil {
		return gostMaterial{}, ErrGostTunnelMaterial
	}
	plaintext, err := r.Vault.Open(sealed)
	if err != nil {
		return gostMaterial{}, ErrGostTunnelMaterial
	}
	material := gostMaterial{}
	if err := json.Unmarshal(plaintext, &material); err != nil {
		return gostMaterial{}, ErrGostTunnelMaterial
	}
	if err := material.valid(tunnelID); err != nil {
		return gostMaterial{}, err
	}
	return material, nil
}

// saveGostMaterial seals one set and stamps the tunnel as modified, so the next
// publication of any drawing that references it re-renders both endpoints.
func (r *Repository) saveGostMaterial(ctx context.Context, e tunnelDatabase, tunnelID string, material *gostMaterial, stamp string) error {
	if material == nil {
		_, err := e.ExecContext(ctx, "UPDATE canvas_tunnels SET gost_material=NULL,updated_at=? WHERE id=?", stamp, tunnelID)
		return err
	}
	sealed, err := r.sealGostMaterial(tunnelID, *material)
	if err != nil {
		return err
	}
	_, err = e.ExecContext(ctx, "UPDATE canvas_tunnels SET gost_material=?,updated_at=? WHERE id=?", sealed, stamp, tunnelID)
	return err
}

func (r *Repository) sealGostMaterial(tunnelID string, material gostMaterial) ([]byte, error) {
	if err := material.valid(tunnelID); err != nil {
		return nil, err
	}
	if r.Vault == nil {
		return nil, errors.New("configuration key unavailable")
	}
	data, err := json.Marshal(material)
	if err != nil {
		return nil, err
	}
	return r.Vault.Seal(data)
}

// RotateCanvasTunnelCredentials replaces the credential set of one GOST tunnel and
// marks it modified. The publication that follows deploys the exit node first and
// the entry node second, so the relay keeps its old certificate for the length of
// one deployment and the tunnel is interrupted briefly; nothing else is staged.
func (s *Service) RotateCanvasTunnelCredentials(ctx context.Context, tunnelID, updatedAt string) (CanvasTunnel, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	if err := s.Repository.ReconcileCanvasTunnels(ctx); err != nil {
		return CanvasTunnel{}, err
	}
	t, err := s.Repository.canvasTunnel(ctx, tunnelID)
	if err != nil {
		return CanvasTunnel{}, err
	}
	if !t.gostRelay() {
		return CanvasTunnel{}, errors.New("only a GOST port_forward tunnel has credentials")
	}
	if t.UpdatedAt != updatedAt {
		return CanvasTunnel{}, ErrStaleScheme
	}
	generation := int64(1)
	if t.material != nil {
		generation = t.material.Generation + 1
	}
	material, err := newGostMaterial(tunnelID, generation)
	if err != nil {
		return CanvasTunnel{}, err
	}
	if err := s.Repository.saveGostMaterial(ctx, s.Repository.Database, tunnelID, &material, nextCanvasStamp(t.UpdatedAt)); err != nil {
		return CanvasTunnel{}, err
	}
	return s.Repository.canvasTunnel(ctx, tunnelID)
}

// syncGostMaterial keeps the sealed column in step with the tunnel kind: a GOST
// port_forward tunnel gets a set the first time it is saved (so publication never
// has to invent one), every other kind keeps none. A rotation replaces the set; an
// ordinary edit preserves it and its generation.
func (r *Repository) syncGostMaterial(ctx context.Context, e tunnelDatabase, tunnelID string, isGost bool, stamp string) error {
	var sealed []byte
	if err := e.QueryRowContext(ctx, "SELECT gost_material FROM canvas_tunnels WHERE id=?", tunnelID).Scan(&sealed); err != nil {
		return err
	}
	switch {
	case isGost && len(sealed) == 0:
		material, err := newGostMaterial(tunnelID, 1)
		if err != nil {
			return err
		}
		return r.saveGostMaterial(ctx, e, tunnelID, &material, stamp)
	case !isGost && len(sealed) != 0:
		return r.saveGostMaterial(ctx, e, tunnelID, nil, stamp)
	}
	return nil
}
