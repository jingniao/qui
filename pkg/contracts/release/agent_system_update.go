package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

var systemUpdateID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var systemUpdateVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// AgentSystemUpdateRequest selects the installed terminal updater; it cannot
// select an executable, download source, environment or filesystem path.
type AgentSystemUpdateRequest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Operation     string `json:"operation"`
	TaskID        string `json:"taskId"`
	AttemptID     string `json:"attemptId"`
	NodeID        string `json:"nodeId"`
	Revision      int64  `json:"revision"`
	TargetVersion string `json:"targetVersion"`
}

func (r AgentSystemUpdateRequest) Validate() error {
	if r.SchemaVersion != 1 || !systemUpdateID.MatchString(r.TaskID) || !systemUpdateID.MatchString(r.AttemptID) || !systemUpdateID.MatchString(r.NodeID) || r.Revision < 0 {
		return fmt.Errorf("invalid system update identity or schema")
	}
	if r.Operation == "probe" && r.TaskID == "probe" && r.AttemptID == "probe" && r.TargetVersion == "" {
		return nil
	}
	if r.Operation != "upgrade" || r.Revision < 1 || !systemUpdateVersion.MatchString(r.TargetVersion) || r.AttemptID == "probe" || r.TaskID == "probe" {
		return fmt.Errorf("invalid system update operation or formal target version")
	}
	return nil
}
func DecodeAgentSystemUpdateRequest(data []byte) (AgentSystemUpdateRequest, error) {
	var r AgentSystemUpdateRequest
	if err := strictUpdate(data, 16384, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}

type AgentSystemUpdateProgress struct {
	AttemptID  string `json:"attemptId"`
	Sequence   uint64 `json:"sequence"`
	Phase      string `json:"phase"`
	ObservedAt string `json:"observedAt"`
	Error      string `json:"error,omitempty"`
}

func DecodeAgentSystemUpdateProgress(data []byte) (AgentSystemUpdateProgress, error) {
	var p AgentSystemUpdateProgress
	if err := strictUpdate(data, 16384, &p); err != nil {
		return p, err
	}
	if !systemUpdateID.MatchString(p.AttemptID) || (p.Sequence == 0 && (p.Phase != "request_submit" || p.Error == "")) || p.Sequence > 1<<63-1 || p.Phase == "" {
		return p, fmt.Errorf("invalid system update progress")
	}
	_, err := time.Parse(time.RFC3339Nano, p.ObservedAt)
	return p, err
}

type AgentSystemUpdateVersion struct {
	Version string `json:"version"`
	Error   string `json:"error"`
}
type AgentSystemUpdateRecovery struct {
	Attempted bool   `json:"attempted"`
	Success   *bool  `json:"success"`
	Error     string `json:"error"`
}
type AgentSystemUpdateStage struct {
	Status   string `json:"status"`
	Error    string `json:"error"`
	ExitCode *int   `json:"exitCode"`
}
type AgentSystemUpdateResult struct {
	SchemaVersion    int                       `json:"schemaVersion"`
	TaskID           string                    `json:"taskId"`
	AttemptID        string                    `json:"attemptId"`
	NodeID           string                    `json:"nodeId"`
	Revision         int64                     `json:"revision"`
	TargetVersion    string                    `json:"targetVersion"`
	Status           string                    `json:"status"`
	Phase            string                    `json:"phase"`
	Error            string                    `json:"error"`
	ExitCode         *int                      `json:"exitCode"`
	CommandExitCode  *int                      `json:"commandExitCode"`
	Signal           string                    `json:"signal"`
	BootstrapStage   AgentSystemUpdateStage    `json:"bootstrapStage"`
	AgentStage       AgentSystemUpdateStage    `json:"agentStage"`
	BootstrapVersion AgentSystemUpdateVersion  `json:"bootstrapVersion"`
	InstalledVersion AgentSystemUpdateVersion  `json:"installedVersion"`
	RunningVersion   AgentSystemUpdateVersion  `json:"runningVersion"`
	ServiceState     string                    `json:"serviceState"`
	ServiceError     string                    `json:"serviceError"`
	Recovery         AgentSystemUpdateRecovery `json:"recovery"`
	StartedAt        string                    `json:"startedAt"`
	FinishedAt       string                    `json:"finishedAt"`
	LogBytes         int64                     `json:"logBytes"`
	LogError         string                    `json:"logError"`
	PersistenceError string                    `json:"persistenceError"`
}

func (r AgentSystemUpdateResult) Validate() error {
	q := AgentSystemUpdateRequest{1, "upgrade", r.TaskID, r.AttemptID, r.NodeID, r.Revision, r.TargetVersion}
	if err := q.Validate(); err != nil {
		return err
	}
	if r.SchemaVersion != 1 || r.Phase == "" || r.LogBytes < 0 {
		return fmt.Errorf("invalid system update result")
	}
	switch r.Status {
	case "success":
		if r.ExitCode == nil || *r.ExitCode != 0 || r.BootstrapVersion.Version != r.TargetVersion || r.InstalledVersion.Version != r.TargetVersion || r.RunningVersion.Version != r.TargetVersion || r.ServiceState != "running" || r.Error != "" || r.LogError != "" || r.PersistenceError != "" || r.BootstrapVersion.Error != "" || r.InstalledVersion.Error != "" || r.RunningVersion.Error != "" || r.ServiceError != "" {
			return fmt.Errorf("system update success lacks terminal version and health evidence")
		}
	case "failed":
		if r.Error == "" {
			return fmt.Errorf("failed system update lacks original error")
		}
	case "unconfirmed":
	default:
		return fmt.Errorf("invalid system update conclusion")
	}
	for _, v := range []string{r.StartedAt, r.FinishedAt} {
		if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
			return fmt.Errorf("invalid system update timestamp: %w", err)
		}
	}
	return nil
}
func DecodeAgentSystemUpdateResult(data []byte) (AgentSystemUpdateResult, error) {
	var r AgentSystemUpdateResult
	if err := strictUpdate(data, 1<<20, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}

type AgentSystemUpdateLogChunk struct {
	AttemptID string `json:"attemptId"`
	Sequence  uint64 `json:"sequence"`
	Offset    int64  `json:"offset"`
	SHA256    string `json:"sha256"`
	Content   []byte `json:"content"`
}

func (c AgentSystemUpdateLogChunk) Validate() error {
	if !systemUpdateID.MatchString(c.AttemptID) || c.Sequence < 1 || c.Offset < 0 || len(c.Content) == 0 || len(c.Content) > 65536 {
		return fmt.Errorf("invalid system update log chunk")
	}
	d := sha256.Sum256(c.Content)
	if hex.EncodeToString(d[:]) != c.SHA256 {
		return fmt.Errorf("system update log checksum mismatch")
	}
	return nil
}
func DecodeAgentSystemUpdateLogChunk(data []byte) (AgentSystemUpdateLogChunk, error) {
	var c AgentSystemUpdateLogChunk
	if err := strictUpdate(data, 100000, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

type AgentSystemUpdateAck struct {
	DispatchErrorSHA256 string `json:"dispatchErrorSha256,omitempty"`
	AttemptID           string `json:"attemptId"`
	LogSequence         uint64 `json:"logSequence"`
	LogOffset           int64  `json:"logOffset"`
	ResultSHA256        string `json:"resultSha256"`
	ProgressSequence    uint64 `json:"progressSequence"`
}
type AgentSystemUpdateProbe struct {
	SchemaVersion    int    `json:"schemaVersion"`
	NodeID           string `json:"nodeId"`
	AttemptID        string `json:"attemptId"`
	Supported        bool   `json:"supported"`
	Reason           string `json:"reason"`
	BootstrapVersion string `json:"bootstrapVersion"`
	UpdaterAvailable bool   `json:"updaterAvailable"`
	WatcherReady     bool   `json:"watcherReady"`
	ObservedAt       string `json:"observedAt"`
}
