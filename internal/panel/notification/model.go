package notification

import (
	"errors"
	"time"
)

var ErrInvalid = errors.New("invalid notification input")

type Rule struct {
	Kind            string  `json:"kind"`
	Enabled         bool    `json:"enabled"`
	Threshold       float64 `json:"threshold"`
	DurationMinutes int     `json:"durationMinutes"`
	CooldownMinutes int     `json:"cooldownMinutes"`
}

func (r Rule) Validate() error {
	if r.CooldownMinutes < 0 || r.CooldownMinutes > 1440 {
		return ErrInvalid
	}
	if r.DurationMinutes < 0 || r.DurationMinutes > 1440 {
		return ErrInvalid
	}
	switch r.Kind {
	case "node_offline":
		// threshold unused
	case "cpu", "memory", "disk":
		if r.Threshold < 1 || r.Threshold > 100 {
			return ErrInvalid
		}
	case "public_ip":
		// threshold unused
	case "machine_traffic":
		if r.Threshold < 1 || r.Threshold > 100 {
			return ErrInvalid
		}
	case "system_alert":
		// threshold unused
	default:
		return ErrInvalid
	}
	return nil
}

type Notification struct {
	ID       int64  `json:"id"`
	At       string `json:"at"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"` // "info" | "warning" | "critical" | "resolved"
	NodeID   string `json:"nodeId,omitempty"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Read     bool   `json:"read"`
}

type ChannelConfig struct {
	BotToken string `json:"botToken,omitempty"`
	ChatID   string `json:"chatId,omitempty"`
	URL      string `json:"url,omitempty"`
}

type Channel struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"` // "telegram" | "webhook"
	Name           string  `json:"name"`
	Enabled        bool    `json:"enabled"`
	Last4          string  `json:"last4"`
	LastResultCode string  `json:"lastResultCode"`
	LastSentAt     *string `json:"lastSentAt"`
}

type State struct {
	Key          string
	Active       bool
	Since        time.Time
	LastNotified time.Time
	Value        string
}
