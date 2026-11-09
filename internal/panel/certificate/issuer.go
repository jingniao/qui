package certificate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
)

// BeginRequest is everything one issuance needs from the panel.
type BeginRequest struct {
	Domains   []string
	Email     string
	Directory string
	// Account is the stored account when it belongs to Directory and Email; an
	// empty URL means the CA must register one first.
	Account Account
}

// Authorization is one pending dns-01 authorization.
type Authorization struct {
	URL    string
	Domain string // the identifier the CA asked about; a wildcard's base name
	Token  string // dns-01 challenge token
	Record string // the TXT value that proves the challenge
	Valid  bool   // already authorized, so nothing has to be published
}

// Issued is one completed order.
type Issued struct {
	Material  Material
	NotBefore time.Time
	NotAfter  time.Time
}

// Order is one open ACME order. The worker publishes the challenge records and
// waits for propagation between Accept and Wait, which is why the DNS side is
// not part of this interface.
type Order interface {
	Authorizations() []Authorization
	Accept(ctx context.Context, authorization Authorization) error
	Wait(ctx context.Context, authorization Authorization) error
	Finalize(ctx context.Context) (Issued, error)
}

// Issuer is the ACME side of one issuance. Production is *ACMEIssuer; tests
// replace it rather than emulating a certificate authority.
type Issuer interface {
	Begin(ctx context.Context, request BeginRequest) (Account, Order, error)
}

// ACMEIssuer performs the protocol with golang.org/x/crypto/acme. Both
// DirectoryURL and Client are injectable so a test can point them elsewhere,
// while the panel uses the fixed Let's Encrypt directories and one 30 second
// HTTP client.
type ACMEIssuer struct {
	DirectoryURL func(directory string) (string, error)
	Client       *http.Client
}

// NewACMEIssuer returns the production issuer.
func NewACMEIssuer() *ACMEIssuer {
	return &ACMEIssuer{DirectoryURL: func(directory string) (string, error) {
		url, ok := DirectoryURL(directory)
		if !ok {
			return "", ErrSettingsInvalid
		}
		return url, nil
	}, Client: &http.Client{Timeout: 30 * time.Second}}
}

// Begin registers the account when needed and opens the order.
func (i *ACMEIssuer) Begin(ctx context.Context, request BeginRequest) (Account, Order, error) {
	resolve := i.DirectoryURL
	if resolve == nil {
		resolve = func(string) (string, error) { return "", ErrSettingsInvalid }
	}
	directoryURL, err := resolve(request.Directory)
	if err != nil {
		return Account{}, nil, fail(CodeACMEAccountFailed)
	}
	key, keyPEM, err := signingKey(request.Account.KeyPEM)
	if err != nil {
		return Account{}, nil, err
	}
	client := &acme.Client{Key: key, DirectoryURL: directoryURL, HTTPClient: i.Client, UserAgent: "qui-panel"}
	account := Account{KeyPEM: keyPEM, Directory: request.Directory, Email: request.Email}
	if request.Account.URL != "" {
		client.KID = acme.KeyID(request.Account.URL)
		account.URL = request.Account.URL
	} else {
		registered, err := client.Register(ctx, &acme.Account{Contact: []string{"mailto:" + request.Email}}, acme.AcceptTOS)
		if errors.Is(err, acme.ErrAccountAlreadyExists) {
			// The key is already registered with this CA, for example because the
			// panel lost the account row but kept nothing else: reuse it.
			registered, err = client.GetReg(ctx, "")
		}
		if err != nil || registered.URI == "" {
			return Account{}, nil, acmeFailure(err, CodeACMEAccountFailed)
		}
		account.URL = registered.URI
		client.KID = acme.KeyID(registered.URI)
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(request.Domains...))
	if err != nil {
		return Account{}, nil, acmeFailure(err, CodeACMEOrderFailed)
	}
	pending := &acmeOrder{client: client, order: order, challenges: map[string]*acme.Challenge{}}
	for _, url := range order.AuthzURLs {
		authorization, err := client.GetAuthorization(ctx, url)
		if err != nil {
			return Account{}, nil, acmeFailure(err, CodeACMEAuthorizationFailed)
		}
		entry := Authorization{URL: url, Domain: authorization.Identifier.Value, Valid: authorization.Status == acme.StatusValid}
		if !entry.Valid {
			for _, challenge := range authorization.Challenges {
				if challenge.Type == "dns-01" {
					entry.Token = challenge.Token
					pending.challenges[url] = challenge
					break
				}
			}
			if entry.Token == "" {
				return Account{}, nil, fail(CodeACMEAuthorizationFailed)
			}
			// The record value is a hash of the token with the account key, so it
			// can only be computed here; the worker publishes it verbatim.
			record, err := client.DNS01ChallengeRecord(entry.Token)
			if err != nil {
				return Account{}, nil, fail(CodeACMEAuthorizationFailed)
			}
			entry.Record = record
		}
		pending.authorizations = append(pending.authorizations, entry)
	}
	return account, pending, nil
}

// acmeFailure maps an ACME problem onto the fixed code of the step that failed,
// recognising the CA's rate limit so the page can say "稍后重试" instead of
// blaming the domain. The problem's own detail is never stored.
func acmeFailure(err error, fallback string) error {
	if err == nil {
		return fail(fallback)
	}
	var problem *acme.Error
	if errors.As(err, &problem) {
		if strings.HasSuffix(problem.ProblemType, ":rateLimited") {
			return fail(CodeACMERateLimited)
		}
		return fail(fallback)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fail(fallback)
}

// signingKey returns the stored P-256 account key, or a new one with its PEM.
func signingKey(stored string) (*ecdsa.PrivateKey, string, error) {
	if stored != "" {
		block, _ := pem.Decode([]byte(stored))
		if block == nil {
			return nil, "", fail(CodeACMEAccountFailed)
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil || key.Curve != elliptic.P256() {
			return nil, "", fail(CodeACMEAccountFailed)
		}
		return key, stored, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fail(CodeACMEAccountFailed)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, "", fail(CodeACMEAccountFailed)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
}

// acmeOrder keeps the open order and the challenge object each authorization
// needs, because accepting a challenge requires its URI and token.
type acmeOrder struct {
	client         *acme.Client
	order          *acme.Order
	authorizations []Authorization
	challenges     map[string]*acme.Challenge
}

func (o *acmeOrder) Authorizations() []Authorization { return o.authorizations }

func (o *acmeOrder) Accept(ctx context.Context, authorization Authorization) error {
	challenge, ok := o.challenges[authorization.URL]
	if !ok {
		return fail(CodeACMEAuthorizationFailed)
	}
	if _, err := o.client.Accept(ctx, challenge); err != nil {
		return acmeFailure(err, CodeACMEAuthorizationFailed)
	}
	return nil
}

func (o *acmeOrder) Wait(ctx context.Context, authorization Authorization) error {
	resolved, err := o.client.WaitAuthorization(ctx, authorization.URL)
	if err != nil {
		return acmeFailure(err, CodeACMEAuthorizationFailed)
	}
	if resolved.Status != acme.StatusValid {
		return fail(CodeACMEAuthorizationFailed)
	}
	return nil
}

// Finalize generates a fresh P-256 certificate key, sends the CSR and returns the
// bundle with the leaf's validity window.
func (o *acmeOrder) Finalize(ctx context.Context) (Issued, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Issued{}, fail(CodeACMEFinalizeFailed)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return Issued{}, fail(CodeACMEFinalizeFailed)
	}
	request, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: o.names()}, key)
	if err != nil {
		return Issued{}, fail(CodeACMEFinalizeFailed)
	}
	bundle, _, err := o.client.CreateOrderCert(ctx, o.order.FinalizeURL, request, true)
	if err != nil || len(bundle) == 0 {
		return Issued{}, acmeFailure(err, CodeACMEFinalizeFailed)
	}
	leaf, err := x509.ParseCertificate(bundle[0])
	if err != nil {
		return Issued{}, fail(CodeACMEFinalizeFailed)
	}
	return Issued{
		Material:  Material{CertificatePEM: chainPEM(bundle), PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))},
		NotBefore: leaf.NotBefore,
		NotAfter:  leaf.NotAfter,
	}, nil
}

// names is the certificate's name list: every identifier the order covers,
// wildcard included, which is what the CA signs into the SAN.
func (o *acmeOrder) names() []string {
	names := make([]string, 0, len(o.order.Identifiers))
	for _, identifier := range o.order.Identifiers {
		names = append(names, identifier.Value)
	}
	return names
}

// chainPEM renders the issued chain, leaf first, as one sequence of PEM blocks.
func chainPEM(bundle [][]byte) string {
	var builder strings.Builder
	for _, der := range bundle {
		builder.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	return builder.String()
}
