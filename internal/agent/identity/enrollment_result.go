package identity

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"time"

	"proxycontrol/internal/platform/pki"
)

const enrollmentResultName = "enrollment-result.json"

type enrollmentResult struct {
	Version       int    `json:"version"`
	TokenDigest   string `json:"tokenDigest"`
	ControlDigest string `json:"controlDigest"`
	NodeID        string `json:"nodeId"`
	Certificate   []byte `json:"certificate"`
	CA            []byte `json:"ca"`
	ExpiresAt     string `json:"expiresAt"`
}
type enrollmentResultEnvelope struct {
	Payload json.RawMessage `json:"payload"`
	MAC     string          `json:"mac"`
}

func enrollmentResultMAC(private, payload []byte) []byte {
	derive := sha256.New()
	derive.Write([]byte("proxycontrol-original-enrollment-result-v1\x00"))
	derive.Write(private)
	key := derive.Sum(nil)
	defer clear(key)
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}
func validateEnrollmentResult(root string, item enrollmentResult, private []byte, tokenDigest, controlDigest string) error {
	if item.Version != 1 || item.TokenDigest != tokenDigest || item.ControlDigest != controlDigest || item.NodeID == "" || len(item.NodeID) > 128 || len(item.Certificate) == 0 || len(item.Certificate) > 32768 || len(item.CA) == 0 || len(item.CA) > 32768 {
		return errors.New("原登记结果绑定或材料预算无效")
	}
	attemptBytes, err := readMaterialFile(filepath.Join(root, enrollmentAttemptName), 4096)
	if err != nil {
		return err
	}
	var attempt enrollmentAttempt
	if pki.DecodeTransitionJSON(attemptBytes, &attempt) != nil || attempt.Version != 1 || attempt.TokenSHA256 != tokenDigest || attempt.ControlSHA256 != controlDigest {
		return errors.New("原登记尝试与结果不匹配")
	}
	pair, err := tls.X509KeyPair(item.Certificate, private)
	if err != nil || len(pair.Certificate) != 1 {
		return errors.New("原登记证书与保留私钥不配对")
	}
	certificateBlock, certificateRest := pem.Decode(item.Certificate)
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" || len(bytes.TrimSpace(certificateRest)) != 0 {
		return errors.New("原登记需要单一证书PEM")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	id, err := pki.NodeID(leaf)
	if err != nil || id != item.NodeID {
		return errors.New("原登记节点绑定不匹配")
	}
	expires, err := time.Parse(time.RFC3339Nano, item.ExpiresAt)
	if err != nil || !expires.Equal(leaf.NotAfter) {
		return errors.New("原登记结果期限不匹配")
	}
	anchor, rest := pem.Decode(item.CA)
	if anchor == nil || anchor.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("原登记CA格式无效")
	}
	ca, err := x509.ParseCertificate(anchor.Bytes)
	if err != nil || !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		return errors.New("原登记CA类型无效")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return errors.New("原登记证书当前时效或签发关系无效")
	}
	return nil
}

// Persist the original TLS-received result before publishing any identity file.
// No key, root or result is regenerated when this immutable record already exists.
func RetainEnrollmentResult(ctx context.Context, root, tokenDigest, controlDigest, nodeID string, certificate, ca []byte, expires time.Time) error {
	private, err := readMaterialFile(filepath.Join(root, "identity", "client.key"), 32768)
	if err != nil {
		return err
	}
	defer clear(private)
	item := enrollmentResult{Version: 1, TokenDigest: tokenDigest, ControlDigest: controlDigest, NodeID: nodeID, Certificate: certificate, CA: ca, ExpiresAt: expires.UTC().Format(time.RFC3339Nano)}
	if err := validateEnrollmentResult(root, item, private, tokenDigest, controlDigest); err != nil {
		return err
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	data, err := json.Marshal(enrollmentResultEnvelope{Payload: payload, MAC: hex.EncodeToString(enrollmentResultMAC(private, payload))})
	if err != nil {
		return err
	}
	if len(data) > 128<<10 {
		return errors.New("原登记结果超过预算")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(root, enrollmentResultName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("原登记结果已存在或无法独占保存，保留材料")
	}
	if _, err = file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Close(); err != nil {
		return err
	}
	return syncAckDirectory(root)
}

func ResumeEnrollmentResult(ctx context.Context, root, tokenDigest, controlDigest string) (bool, error) {
	data, err := readMaterialFile(filepath.Join(root, enrollmentResultName), 128<<10)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	private, err := readMaterialFile(filepath.Join(root, "identity", "client.key"), 32768)
	if err != nil {
		return true, err
	}
	defer clear(private)
	var envelope enrollmentResultEnvelope
	if pki.DecodeTransitionJSON(data, &envelope) != nil || len(envelope.Payload) == 0 {
		return true, errors.New("原登记结果格式无效，保留材料")
	}
	mac, err := hex.DecodeString(envelope.MAC)
	if err != nil || len(mac) != 32 || hex.EncodeToString(mac) != envelope.MAC || !hmac.Equal(mac, enrollmentResultMAC(private, envelope.Payload)) {
		return true, errors.New("原私钥不能认证原登记结果，保留材料")
	}
	var item enrollmentResult
	if pki.DecodeTransitionJSON(envelope.Payload, &item) != nil {
		return true, errors.New("原登记结果正文无效")
	}
	if err := validateEnrollmentResult(root, item, private, tokenDigest, controlDigest); err != nil {
		return true, err
	}
	if err := requireEnrollmentResultDirectory(ctx, root); err != nil {
		return true, err
	}
	directory := filepath.Join(root, "identity")
	state := &State{Directory: directory, NodeID: item.NodeID}
	if err := state.requireAckMaterial(0); err != nil {
		return true, err
	}
	observation, err := captureEnrollmentObservation(ctx, root)
	if err != nil {
		return true, err
	}
	defer observation.clear()
	if !observation.matches(enrollmentResultName, data) || !observation.matches("identity/client.key", private) {
		return true, errors.New("原结果或私钥在认证期间变化")
	}
	// Inspect all existing targets before filling any missing file.
	targets := []struct {
		name string
		data []byte
	}{{"client.crt", item.Certificate}, {"node-ca.crt", item.CA}}
	for _, target := range targets {
		existing, err := readMaterialFile(filepath.Join(directory, target.name), 32768)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return true, err
		}
		if !bytes.Equal(existing, target.data) {
			return true, errors.New("登记目标与原结果冲突，保留所有材料")
		}
	}
	latestPrivate, err := readMaterialFile(filepath.Join(directory, "client.key"), 32768)
	if err != nil {
		return true, err
	}
	samePrivate := bytes.Equal(latestPrivate, private)
	clear(latestPrivate)
	if !samePrivate {
		return true, errors.New("原登记私钥在接续期间变化")
	}
	latestResult, err := readMaterialFile(filepath.Join(root, enrollmentResultName), 128<<10)
	if err != nil || !bytes.Equal(latestResult, data) {
		return true, errors.New("原登记结果在接续期间变化")
	}
	if err := validateEnrollmentResult(root, item, private, tokenDigest, controlDigest); err != nil {
		return true, err
	}
	// Reconfirm persistence of a result whose previous directory sync failed.
	resultFile, err := os.OpenFile(filepath.Join(root, enrollmentResultName), os.O_RDWR, 0)
	if err != nil {
		return true, err
	}
	if err := errors.Join(resultFile.Sync(), resultFile.Close()); err != nil {
		return true, err
	}
	if err := syncAckDirectory(root); err != nil {
		return true, err
	}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		if err := observation.recheck(ctx, root); err != nil {
			return true, err
		}
		if err := requireEnrollmentResultDirectory(ctx, root); err != nil {
			return true, err
		}
		if err := fillOriginalEnrollmentTarget(directory, target.name, target.data); err != nil {
			return true, err
		}
		observation.expect("identity/"+target.name, target.data)
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if err := state.requireAckMaterial(0); err != nil {
		return true, err
	}
	if err := observation.recheck(ctx, root); err != nil {
		return true, err
	}
	if err := validateEnrollmentResult(root, item, private, tokenDigest, controlDigest); err != nil {
		return true, err
	}
	if _, err := Load(directory); errors.Is(err, os.ErrNotExist) {
		if err := state.Save(0); err != nil {
			return true, err
		}
		original, err := json.Marshal(ackIdentity{NodeID: item.NodeID, LastAcked: 0})
		if err != nil {
			return true, err
		}
		observation.expect("identity/identity.json", original)
	} else if err != nil {
		return true, err
	}
	if err := syncAckDirectory(directory); err != nil {
		return true, err
	}
	if err := observation.recheck(ctx, root); err != nil {
		return true, err
	}
	if err := requireEnrollmentResultDirectory(ctx, root); err != nil {
		return true, err
	}
	if err := validateEnrollmentResult(root, item, private, tokenDigest, controlDigest); err != nil {
		return true, err
	}
	return true, ctx.Err()
}

func fillOriginalEnrollmentTarget(directory, name string, expected []byte) error {
	path := filepath.Join(directory, name)
	existing, err := readMaterialFile(path, 32768)
	if err == nil {
		if !bytes.Equal(existing, expected) {
			return errors.New("登记目标变化，不覆盖")
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		return errors.Join(file.Sync(), file.Close())
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(expected); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	return file.Close()
}
