package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type ackIdentity struct {
	NodeID    string `json:"node_id"`
	LastAcked uint64 `json:"last_acked"`
}

func parseAckIdentity(data []byte) (ackIdentity, error) {
	var result ackIdentity
	invalid := errors.New("invalid original acknowledgment identity material")
	if len(data) == 0 || len(data) > 4096 {
		return result, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return result, invalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return result, invalid
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return result, invalid
		}
		seen[key] = true
		switch key {
		case "node_id":
			if decoder.Decode(&result.NodeID) != nil {
				return result, invalid
			}
		case "last_acked":
			var raw json.RawMessage
			if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &result.LastAcked) != nil {
				return result, invalid
			}
		default:
			return result, invalid
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || !seen["node_id"] || !seen["last_acked"] || result.NodeID == "" || len(result.NodeID) > 128 || result.LastAcked > uint64(1<<63-1) {
		return result, invalid
	}
	return result, nil
}

// Original ACK material is bounded and checked before a replacement. A
// published-but-unconfirmed value must never be overwritten by old memory.
func (s *State) requireAckMaterial(ack uint64) error {
	path := filepath.Join(s.Directory, "identity.json")
	data, err := readState(path)
	if errors.Is(err, os.ErrNotExist) {
		if s.ackObserved || s.LastAcked != 0 {
			return errors.New("original acknowledgment file missing")
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer clear(data)
	original, err := parseAckIdentity(data)
	if err != nil {
		return err
	}
	if original.NodeID != s.NodeID || original.LastAcked > ack || original.LastAcked < s.LastAcked {
		return errors.New("original acknowledgment identity or position conflicts")
	}
	return nil
}
