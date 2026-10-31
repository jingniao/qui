package portal

import "time"

// Profile contains only information the signed-in user is allowed to see.
type Profile struct {
	ID                          string   `json:"id"`
	Username                    string   `json:"username"`
	DisplayName                 string   `json:"displayName"`
	Status                      string   `json:"status"`
	Reason                      string   `json:"reason"`
	TrafficLimit                int64    `json:"trafficLimit"`
	UsedTraffic                 int64    `json:"usedTraffic"`
	ExpiresAt                   string   `json:"expiresAt"`
	AllowedProtocols            []string `json:"allowedProtocols"`
	DeviceLimitEnforced         bool     `json:"deviceLimitEnforced"`
	DeviceLimitUnsupportedNodes []string `json:"deviceLimitUnsupportedNodes"`
	DeviceLimitUDPUnsupported   []string `json:"deviceLimitUdpUnsupportedProtocols"`
	DeviceLimit                 *int     `json:"deviceLimit"`
	OnlineDevices               *int     `json:"onlineDevices"`
	WireGuardNote               string   `json:"wireGuardNote"`
}

type Session struct {
	IsAuthenticated bool      `json:"isAuthenticated"`
	CurrentUser     Profile   `json:"currentUser"`
	CSRFToken       string    `json:"csrfToken"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"region"`
	Online bool   `json:"online"`
}
