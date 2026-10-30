package traffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type Counter struct {
	// Kind=node is a per-core aggregate, never a user quota delta. It is sent
	// only to peers negotiating multi_core_supported. Empty keeps the v1 shape.
	Kind          string    `json:"kind,omitempty"`
	Core          string    `json:"core,omitempty"`
	CoreEpoch     string    `json:"core_epoch"`
	Revision      int64     `json:"revision"`
	CredentialID  string    `json:"credential_id"`
	UserID        string    `json:"user_id"`
	DeviceID      string    `json:"device_id"`
	Protocol      string    `json:"protocol"`
	UploadBytes   int64     `json:"upload_bytes"`
	DownloadBytes int64     `json:"download_bytes"`
	ObservedAt    time.Time `json:"observed_at"`
}
type Batch struct {
	ID       string    `json:"id"`
	NodeID   string    `json:"node_id"`
	Counters []Counter `json:"counters"`
}

func validID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return value != "." && value != ".."
}

const MaxBatchCounters = 256

// Native sampling may cover more accounts than one bounded wire batch.
const MaxSampleCounters = 16384

func ValidateSample(nodeID string, counters []Counter) error {
	if !validID(nodeID) || len(counters) < 1 || len(counters) > MaxSampleCounters {
		return errors.New("invalid native traffic sample")
	}
	return validateCounters(counters)
}
func (b *Batch) Validate() error {
	if !validID(b.ID) || !validID(b.NodeID) || len(b.Counters) < 1 || len(b.Counters) > MaxBatchCounters {
		return errors.New("invalid traffic batch")
	}
	return validateCounters(b.Counters)
}
func validateCounters(counters []Counter) error {
	seen := map[string]bool{}
	for index := range counters {
		counter := &counters[index]
		if counter.Kind != "" && counter.Kind != "node" {
			return errors.New("invalid traffic counter kind")
		}
		if counter.Kind == "node" {
			if (counter.Core != "xray" && counter.Core != "sing-box") || counter.CredentialID != "node" || counter.UserID != "node" || counter.DeviceID != "node" || counter.Protocol != "" {
				return errors.New("invalid node traffic counter")
			}
		} else if counter.Core != "" {
			return errors.New("unexpected account core")
		}
		if !validID(counter.CoreEpoch) || !validID(counter.CredentialID) || !validID(counter.UserID) || !validID(counter.DeviceID) || counter.Revision < 1 || counter.UploadBytes < 0 || counter.DownloadBytes < 0 || counter.ObservedAt.IsZero() || counter.ObservedAt.Year() < 2000 || counter.ObservedAt.Year() > 9999 {
			return errors.New("invalid cumulative traffic counter")
		}
		if counter.Kind != "node" && !strings.Contains("|http|socks5|shadowsocks|vless|trojan|wireguard|hysteria2|tuic|tun|", "|"+counter.Protocol+"|") {
			return errors.New("invalid traffic protocol")
		}
		key := counter.CoreEpoch + ":" + counter.CredentialID
		if seen[key] {
			return errors.New("duplicate traffic counter")
		}
		seen[key] = true
		counter.ObservedAt = counter.ObservedAt.UTC()
	}
	return nil
}
func Decode(data []byte) (Batch, error) {
	var batch Batch
	if len(data) == 0 || len(data) > 1<<20 {
		return batch, errors.New("invalid traffic batch size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		return batch, errors.New("invalid traffic batch JSON")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return batch, errors.New("traffic batch must contain one object")
	}
	return batch, batch.Validate()
}
