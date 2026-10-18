package core

import (
	"context"
	"proxycontrol/internal/agent/collector"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

type RenderedConfig struct {
	RuntimeAccountsComplete bool
	NodeID                  string
	Revision                int64
	Data                    []byte
	HealthEndpoints         []string
	UDPHealthEndpoints      []string
	Probes                  []Probe
	AccessContext           *collector.Context
	// AccessLogPath names the core's access log file inside the Agent runtime
	// directory. The adapter tails it and truncates it at its cap; an empty
	// path means this configuration renders no access log.
	AccessLogPath string
	StatsEndpoint string
	StatsAccounts map[string]traffic.Counter
	// OnlineStats reports that this configuration rendered the core's per-user
	// online statistics (Xray policy.levels.0.statsUserOnline). When it is false
	// the online count is unknown, never zero.
	OnlineStats       bool
	ProbeFingerprints map[string]string
}
type Probe struct {
	OutboundID string
	Address    string
	URL        string
	Interval   time.Duration
}
type HealthStatus struct {
	Healthy   bool
	CheckedAt time.Time
}
type ApplyResult struct {
	Revision   int64
	Success    bool
	RolledBack bool
	Code       string
	Message    string
}
type StatsBatch struct{ Upload, Download, Connections uint64 }
type Adapter interface {
	Name() string
	DetectCapabilities(context.Context) (*pb.Capabilities, error)
	Render(context.Context, desiredstate.State) (RenderedConfig, error)
	Validate(context.Context, RenderedConfig) error
	Apply(context.Context, RenderedConfig) (ApplyResult, error)
	Rollback(context.Context, int64) error
	Health(context.Context) (HealthStatus, error)
	CollectStats(context.Context) (StatsBatch, error)
}
