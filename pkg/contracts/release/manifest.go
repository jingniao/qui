// Package release defines signed executable releases. It never supplies paths,
// scripts or process arguments to a node.
package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

const MaxManifestBytes = 16384
const MaxArtifactBytes int64 = 1 << 30

var ErrManifest = errors.New("invalid release manifest")
var ErrSignature = errors.New("release signature is not trusted")
var ErrPolicy = errors.New("release does not satisfy installed trust policy")
var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+\-]{0,63}$`)

type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	Component     string    `json:"component"`
	Version       string    `json:"version"`
	OS            string    `json:"os"`
	Architecture  string    `json:"architecture"`
	Sequence      int64     `json:"sequence"`
	Bytes         int64     `json:"bytes"`
	SHA256        string    `json:"sha256"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}
type Signed struct {
	Manifest  Manifest `json:"manifest"`
	KeyID     string   `json:"key_id"`
	Signature string   `json:"signature"`
}
type TrustedKey struct {
	PublicKey  ed25519.PublicKey
	Components []string
	Revoked    bool
}
type Policy struct {
	Component, OS, Architecture string
	MinimumSequence             int64
	Now                         time.Time
	Keys                        map[string]TrustedKey
}

func Component(value string) bool {
	return value == "proxy-agent" || value == "xray" || value == "sing-box"
}
func KeyID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])
}
func (m Manifest) Validate() error {
	digest, err := hex.DecodeString(m.SHA256)
	if m.SchemaVersion != 1 || !Component(m.Component) || !versionPattern.MatchString(m.Version) || m.Version == "." || m.Version == ".." || m.OS != "linux" && m.OS != "windows" || m.Architecture != "amd64" && m.Architecture != "arm64" || m.Sequence < 1 || m.Bytes < 1 || m.Bytes > MaxArtifactBytes || err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != m.SHA256 {
		return ErrManifest
	}
	if m.IssuedAt.IsZero() || m.ExpiresAt.IsZero() || m.IssuedAt.Year() < 2000 || m.IssuedAt.Year() > 9999 || m.ExpiresAt.Year() > 9999 || m.IssuedAt.Location() != time.UTC || m.ExpiresAt.Location() != time.UTC || !m.ExpiresAt.After(m.IssuedAt) || m.ExpiresAt.Sub(m.IssuedAt) > 366*24*time.Hour {
		return ErrManifest
	}
	return nil
}
func (m Manifest) canonical() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append([]byte("proxy-control executable release v1\n"), data...), nil
}
func Sign(m Manifest, private ed25519.PrivateKey) (Signed, error) {
	if len(private) != ed25519.PrivateKeySize {
		return Signed{}, ErrSignature
	}
	public := private.Public().(ed25519.PublicKey)
	// Reject malformed private keys with a public half unrelated to the seed.
	derived := ed25519.NewKeyFromSeed(private[:ed25519.SeedSize])
	if !bytes.Equal(derived, private) {
		return Signed{}, ErrSignature
	}
	data, err := m.canonical()
	if err != nil {
		return Signed{}, err
	}
	return Signed{Manifest: m, KeyID: KeyID(public), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, data))}, nil
}
func (s Signed) Verify(policy Policy) error {
	data, err := s.Manifest.canonical()
	if err != nil {
		return err
	}
	key, ok := policy.Keys[s.KeyID]
	if !ok || key.Revoked || len(key.PublicKey) != ed25519.PublicKeySize || KeyID(key.PublicKey) != s.KeyID {
		return ErrSignature
	}
	signature, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(signature) != s.Signature || !ed25519.Verify(key.PublicKey, data, signature) {
		return ErrSignature
	}
	allowed := false
	for _, component := range key.Components {
		if component == s.Manifest.Component {
			allowed = true
		}
	}
	if !allowed || s.Manifest.Component != policy.Component || s.Manifest.OS != policy.OS || s.Manifest.Architecture != policy.Architecture || policy.MinimumSequence < 0 || s.Manifest.Sequence < policy.MinimumSequence || policy.Now.IsZero() || s.Manifest.IssuedAt.After(policy.Now.Add(5*time.Minute)) || !policy.Now.Before(s.Manifest.ExpiresAt) {
		return ErrPolicy
	}
	return nil
}
func Decode(data []byte) (Signed, error) {
	var signed Signed
	if len(data) == 0 || len(data) > MaxManifestBytes {
		return signed, ErrManifest
	}
	if err := uniqueJSONKeys(data); err != nil {
		return signed, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&signed); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return signed, ErrManifest
	}
	if err := signed.Manifest.Validate(); err != nil {
		return signed, err
	}
	if len(signed.KeyID) != 64 || len(signed.Signature) > 128 {
		return signed, ErrManifest
	}
	return signed, nil
}

// Duplicate keys have different interpretations across JSON implementations;
// reject them at every nesting level before typed decoding or signature checks.
func uniqueJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 8 {
			return ErrManifest
		}
		token, err := decoder.Token()
		if err != nil {
			return ErrManifest
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return ErrManifest
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return ErrManifest
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return ErrManifest
			}
		default:
			return ErrManifest
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrManifest
	}
	return nil
}
