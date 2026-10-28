package user

import (
	"errors"
	"proxycontrol/pkg/contracts/credential"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrNotFound = errors.New("user or device not found")
var ErrConflict = errors.New("username already exists")
var ErrDeviceLimit = errors.New("maximum device count reached")
var ErrInactive = errors.New("user is disabled, expired or over quota")
var ErrCredential = errors.New("credential unavailable")

type User struct {
	TrafficResetCycle           string   `json:"trafficResetCycle"`
	TrafficResetAnchor          string   `json:"trafficResetAnchor"`
	TrafficResetAt              string   `json:"trafficResetAt"`
	DeviceLimit                 int      `json:"deviceLimit"`
	OnlineDevices               *int     `json:"onlineDevices"`
	DeviceLimitEnforced         bool     `json:"deviceLimitEnforced"`
	DeviceLimitUnsupportedNodes []string `json:"deviceLimitUnsupportedNodes"`
	LoginEnabled                bool     `json:"loginEnabled"`
	ID                          string   `json:"id"`
	Username                    string   `json:"username"`
	DisplayName                 string   `json:"displayName"`
	Status                      string   `json:"status"`
	ExpiresAt                   string   `json:"expiresAt"`
	TrafficLimit                int64    `json:"trafficLimit"`
	UsedTraffic                 int64    `json:"usedTraffic"`
	// MaxDevices and MaxConnections are the retired v1.0 limits (plan C10).
	// The columns stay for their CHECK constraints and older backups; the API
	// no longer carries them.
	MaxDevices       int      `json:"-"`
	MaxConnections   int      `json:"-"`
	AllowedProtocols []string `json:"allowedProtocols"`
	Remark           string   `json:"remark"`
	CreatedAt        string   `json:"createdAt"`
	DevicesCount     int      `json:"-"`
	NodeGroupIDs     []string `json:"nodeGroupIds"`
	// OnlineConnections is the number of live connections the user's active
	// credentials currently hold, summed over the nodes that reported a fresh
	// snapshot. It is null when no reporting node covers the user, so the UI can
	// show "unknown" instead of a false zero.
	OnlineConnections *int `json:"onlineConnections"`
}
type Input struct {
	TrafficResetCycle  string `json:"trafficResetCycle"`
	TrafficResetAnchor string `json:"trafficResetAnchor"`
	DeviceLimit        int    `json:"deviceLimit"`
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	Status             string `json:"status"`
	ExpiresAt          string `json:"expiresAt"`
	TrafficLimit       int64  `json:"trafficLimit"`
	// MaxDevices and MaxConnections are retired (plan C10) and ignored. They
	// are not part of the API: a request that still sends them is refused as an
	// unknown field.
	MaxDevices       int      `json:"-"`
	MaxConnections   int      `json:"-"`
	AllowedProtocols []string `json:"allowedProtocols"`
	Remark           string   `json:"remark"`
	NodeGroupIDs     []string `json:"nodeGroupIds"`
}
type Device struct {
	ID           string       `json:"id"`
	UserID       string       `json:"userId"`
	Name         string       `json:"name"`
	Status       string       `json:"status"`
	CreatedAt    string       `json:"createdAt"`
	LastOnlineAt string       `json:"lastOnlineAt"`
	LastNodeID   string       `json:"lastNodeId"`
	UsedTraffic  int64        `json:"usedTraffic"`
	Credentials  []Credential `json:"credentials"`
}
type Credential struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	DeviceID  string `json:"deviceId"`
	Protocol  string `json:"protocol"`
	Username  string `json:"username"`
	Secret    string `json:"secret,omitempty"`
	PublicKey string `json:"publicKey,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}
type Secret = credential.Secret
type DeviceInput struct {
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Protocols []string `json:"protocols"`
}

func ValidateProtocols(protocols []string) error {
	if len(protocols) == 0 || len(protocols) > 8 {
		return errors.New("choose one to eight supported protocols")
	}
	seen := map[string]bool{}
	for _, protocol := range protocols {
		if !strings.Contains("|http|socks5|shadowsocks|vless|trojan|wireguard|hysteria2|tuic|", "|"+protocol+"|") || seen[protocol] {
			return errors.New("invalid or duplicate protocol")
		}
		seen[protocol] = true
	}
	return nil
}
func (i *Input) Validate() error {
	if i.TrafficResetCycle == "" {
		i.TrafficResetCycle = "none"
	}
	if i.TrafficResetCycle != "none" && i.TrafficResetCycle != "monthly" && i.TrafficResetCycle != "30days" {
		return errors.New("invalid traffic reset cycle")
	}
	if i.TrafficResetAnchor != "" {
		at, err := time.Parse(time.RFC3339Nano, i.TrafficResetAnchor)
		if err != nil || at.Year() < 2000 || at.Year() > 9998 {
			return errors.New("invalid traffic reset anchor")
		}
		i.TrafficResetAnchor = timestamp(at)
	}
	if i.DeviceLimit < 0 || i.DeviceLimit > 100 {
		return errors.New("invalid device limit: expected 0 to 100")
	}
	i.Username = strings.TrimSpace(i.Username)
	i.DisplayName = strings.TrimSpace(i.DisplayName)
	if len(i.Username) < 3 || len(i.Username) > 64 {
		return errors.New("username must contain 3 to 64 ASCII characters")
	}
	for _, r := range i.Username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return errors.New("invalid username")
		}
	}
	if i.DisplayName == "" {
		i.DisplayName = i.Username
	}
	if utf8.RuneCountInString(i.DisplayName) > 80 || len(i.Remark) > 1024 {
		return errors.New("display name or remark too long")
	}
	if i.Status == "" {
		i.Status = "active"
	}
	if i.Status != "active" && i.Status != "disabled" {
		return errors.New("invalid user status")
	}
	if i.ExpiresAt != "" {
		value, err := time.Parse(time.RFC3339, i.ExpiresAt)
		if err != nil {
			return errors.New("expiry must be RFC3339 with timezone")
		}
		i.ExpiresAt = timestamp(value)
	}
	if i.TrafficLimit < 0 {
		return errors.New("invalid quota")
	}
	return ValidateProtocols(i.AllowedProtocols)
}
func (u User) Active(at time.Time) bool { return InactivityReason(u, at) == "" }
func timestamp(value time.Time) string  { return value.UTC().Format("2006-01-02T15:04:05.000000000Z") }
