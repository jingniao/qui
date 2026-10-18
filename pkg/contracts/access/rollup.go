// Package access defines content-free access telemetry shared by Agent and panel.
package access

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/netip"
	policy "proxycontrol/pkg/contracts/policy"
	"time"
)

type Rollup struct {
	Bucket            time.Time `json:"bucket"`
	NodeID            string    `json:"node_id"`
	Revision          int64     `json:"revision"`
	UserID            string    `json:"user_id,omitempty"`
	DeviceID          string    `json:"device_id,omitempty"`
	InboundProtocol   string    `json:"inbound_protocol"`
	Domain            string    `json:"domain,omitempty"`
	TargetIP          string    `json:"target_ip,omitempty"`
	TargetPort        int       `json:"target_port"`
	Network           string    `json:"network"`
	RuleID            string    `json:"rule_id,omitempty"`
	Action            string    `json:"action"`
	OutboundID        string    `json:"outbound_id,omitempty"`
	OutboundProtocol  string    `json:"outbound_protocol,omitempty"`
	ChainID           string    `json:"chain_id,omitempty"`
	FinalEgressNodeID string    `json:"final_egress_node_id,omitempty"`
	EgressKnown       bool      `json:"egress_known"`
	Allowed           bool      `json:"allowed"`
	Connections       int64     `json:"connections"`
	UploadBytes       int64     `json:"upload_bytes"`
	DownloadBytes     int64     `json:"download_bytes"`
	TrafficKnown      bool      `json:"traffic_known"`
}

func identifier(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return value != "." && value != ".."
}

func (r *Rollup) Normalize() error {
	if r.Bucket.IsZero() || r.Bucket.Year() < 2000 || r.Bucket.Year() > 9999 || r.Revision < 1 {
		return errors.New("invalid access time or revision")
	}
	r.Bucket = r.Bucket.UTC().Truncate(time.Minute)
	for _, value := range []string{r.UserID, r.DeviceID, r.RuleID, r.OutboundID, r.ChainID, r.FinalEgressNodeID} {
		if !identifier(value, false) {
			return errors.New("invalid access resource identifier")
		}
	}
	if !identifier(r.NodeID, true) || r.DeviceID != "" && r.UserID == "" {
		return errors.New("invalid access attribution")
	}
	if r.Domain != "" {
		if ip, err := netip.ParseAddr(r.Domain); err == nil {
			if r.TargetIP == "" {
				r.TargetIP = ip.String()
			}
			r.Domain = ""
		}
	}
	if r.Domain != "" {
		domain, err := policy.Domain(r.Domain, false)
		if err != nil {
			return errors.New("access domain must be a hostname without URL or content")
		}
		r.Domain = domain
	}
	if r.TargetIP != "" {
		ip, err := netip.ParseAddr(r.TargetIP)
		if err != nil || ip.Zone() != "" {
			return errors.New("invalid access target IP")
		}
		r.TargetIP = ip.Unmap().String()
	}
	if r.Domain == "" && r.TargetIP == "" || r.TargetPort < 1 || r.TargetPort > 65535 || r.Network != "tcp" && r.Network != "udp" {
		return errors.New("access requires a known domain or IP, port and transport")
	}
	if r.Action != "Direct" && r.Action != "Block" && r.Action != "Proxy" {
		return errors.New("invalid access action")
	}
	if (r.Action == "Block") == r.Allowed {
		return errors.New("access action and outcome disagree")
	}
	if !r.Allowed && (r.FinalEgressNodeID != "" || r.EgressKnown) {
		return errors.New("blocked access cannot declare a final exit")
	}
	if r.EgressKnown && r.FinalEgressNodeID == "" {
		return errors.New("known exit requires its node identity")
	}
	if !r.EgressKnown && r.FinalEgressNodeID != "" {
		return errors.New("unconfirmed exit must not name a node")
	}
	if r.Connections < 0 || r.UploadBytes < 0 || r.DownloadBytes < 0 || r.Connections == 0 && (!r.TrafficKnown || r.UploadBytes == 0 && r.DownloadBytes == 0) {
		return errors.New("invalid access counters")
	}
	if !r.TrafficKnown && (r.UploadBytes != 0 || r.DownloadBytes != 0) {
		return errors.New("unknown traffic cannot carry invented counters")
	}
	if !protocol(r.InboundProtocol, false) || !protocol(r.OutboundProtocol, true) {
		return errors.New("invalid access protocol")
	}
	return nil
}

func protocol(value string, optional bool) bool {
	if value == "" {
		return optional
	}
	switch value {
	case "http", "socks5", "shadowsocks", "vless", "trojan", "wireguard", "hysteria2", "tuic", "tun", "direct", "block":
		return true
	}
	return false
}

// Key preserves every attribution and decision dimension. Traffic availability
// is separate so an unavailable measurement never gets merged into known data.
func (r Rollup) Key() string {
	r.Connections = 0
	r.UploadBytes = 0
	r.DownloadBytes = 0
	data, _ := json.Marshal(r)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func Merge(existing, next Rollup) (Rollup, error) {
	if err := existing.Normalize(); err != nil {
		return Rollup{}, err
	}
	if err := next.Normalize(); err != nil {
		return Rollup{}, err
	}
	if existing.Key() != next.Key() {
		return Rollup{}, errors.New("different access aggregation dimensions")
	}
	if math.MaxInt64-existing.Connections < next.Connections || math.MaxInt64-existing.UploadBytes < next.UploadBytes || math.MaxInt64-existing.DownloadBytes < next.DownloadBytes {
		return Rollup{}, errors.New("access counters overflow")
	}
	existing.Connections += next.Connections
	existing.UploadBytes += next.UploadBytes
	existing.DownloadBytes += next.DownloadBytes
	return existing, nil
}
