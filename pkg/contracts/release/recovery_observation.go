package release

type RecoveryInstallation struct {
	Images     []RecoveryImageObservation `json:"images,omitempty"`
	Component  string                     `json:"component"`
	Generation int64                      `json:"generation"`
	Phase      string                     `json:"phase"`
	RequestID  string                     `json:"requestId"`
	Revision   int64                      `json:"revision"`
	Digest     string                     `json:"digest"`
	Version    string                     `json:"version"`
	Sequence   int64                      `json:"sequence"`
}
type RecoveryImageObservation struct {
	Role     string `json:"role"`
	Digest   string `json:"digest"`
	Bytes    int64  `json:"bytes"`
	Sequence int64  `json:"sequence"`
	Status   string `json:"status"`
}
type RecoveryUpdateHistory struct {
	AcceptedReleases []Manifest             `json:"acceptedReleases,omitempty"`
	Attempts         []RecoveryAttempt      `json:"attempts,omitempty"`
	Kind             string                 `json:"kind"`
	Present          bool                   `json:"present"`
	EventCount       int64                  `json:"eventCount"`
	HeadDigest       string                 `json:"headDigest"`
	Installations    []RecoveryInstallation `json:"installations"`
	PendingJobs      int                    `json:"pendingJobs"`
	StartedJobs      int                    `json:"startedJobs"`
	NeedsRecovery    bool                   `json:"needsRecovery"`
}
type RecoveryAttempt struct {
	RequestDigest   string        `json:"requestDigest,omitempty"`
	Kind            string        `json:"kind"`
	ID              string        `json:"id"`
	OriginalID      string        `json:"originalId,omitempty"`
	Component       string        `json:"component"`
	Revision        int64         `json:"revision"`
	Phase           string        `json:"phase"`
	Operation       string        `json:"operation,omitempty"`
	TargetDigest    string        `json:"targetDigest"`
	InstalledDigest string        `json:"installedDigest,omitempty"`
	Started         bool          `json:"started"`
	Result          *UpdateResult `json:"result,omitempty"`
}
