package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// RecoveryRequestDigest commits to the entire typed original request, including
// the complete signature. It is a reference for signed observations, not a
// signature verification or permission to replay a command.
func RecoveryRequestDigest(request UpdateRequest) (string, error) {
	var err error
	if request.Release.Manifest.Component == "proxy-agent" {
		err = AgentUpdateRequest(request).Validate()
	} else {
		err = request.Validate()
	}
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > MaxUpdateRequestBytes {
		return "", ErrUpdate
	}
	h := sha256.New()
	h.Write([]byte("proxycontrol-recovery-update-request-v1\n"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}
