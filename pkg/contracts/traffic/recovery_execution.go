package traffic

import (
	"encoding/hex"
	"errors"
	"proxycontrol/pkg/contracts/release"
	"time"
)

type RecoveryTrafficExecution struct {
	ID                 string `json:"id"`
	CaseID             string `json:"caseId"`
	IssueID            string `json:"issueId"`
	DecisionID         string `json:"decisionId"`
	NodeID             string `json:"nodeId"`
	BatchID            string `json:"batchId"`
	BatchDigest        string `json:"batchDigest"`
	SnapshotDigest     string `json:"snapshotDigest"`
	IssueDigest        string `json:"issueDigest"`
	EvidenceDigest     string `json:"evidenceDigest"`
	Owner              string `json:"owner"`
	RequestKey         string `json:"requestKey"`
	Status             string `json:"status"`
	Attempts           int64  `json:"attempts"`
	ClaimToken         string `json:"-"`
	LeaseUntil         string `json:"leaseUntil"`
	ResultCode         string `json:"resultCode"`
	CompletedAt        string `json:"completedAt"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
	OperationCompleted bool   `json:"operationCompleted"`
	Settled            bool   `json:"settled"`
	RecoveryPermission bool   `json:"recoveryPermission"`
}

func validRecoveryTrafficDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
func (r *RecoveryTrafficExecution) ValidateStored() error {
	for _, digest := range []string{r.IssueID, r.BatchDigest, r.SnapshotDigest, r.IssueDigest, r.EvidenceDigest} {
		if !validRecoveryTrafficDigest(digest) {
			return errors.New("invalid persisted recovery traffic execution")
		}
	}
	for _, id := range []string{r.ID, r.CaseID, r.DecisionID} {
		if len(id) != 32 || !release.ValidUpdateID(id) {
			return errors.New("invalid persisted recovery traffic execution")
		}
	}
	if r.Owner == "" || len(r.NodeID) > 128 || len(r.BatchID) > 128 || !release.ValidUpdateID(r.NodeID) || !release.ValidUpdateID(r.BatchID) || r.Attempts < 1 || r.Attempts > 2147483647 {
		return errors.New("invalid persisted recovery traffic execution")
	}
	if len(r.RequestKey) < 16 || len(r.RequestKey) > 64 || !release.ValidUpdateID(r.RequestKey) || len(r.ResultCode) > 128 {
		return errors.New("invalid persisted recovery traffic execution")
	}
	for _, stamp := range []string{r.CreatedAt, r.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			return errors.New("invalid persisted recovery traffic execution")
		}
	}
	switch r.Status {
	case "running":
		if r.ClaimToken == "" || r.LeaseUntil == "" || r.CompletedAt != "" {
			return errors.New("invalid persisted recovery traffic execution")
		}
		if _, err := time.Parse(time.RFC3339Nano, r.LeaseUntil); err != nil {
			return errors.New("invalid persisted recovery traffic execution")
		}
	case "pending", "not_confirmed":
		if r.ClaimToken != "" || r.LeaseUntil != "" || r.CompletedAt != "" {
			return errors.New("invalid persisted recovery traffic execution")
		}
	case "completed":
		if r.ClaimToken != "" || r.LeaseUntil != "" {
			return errors.New("invalid persisted recovery traffic execution")
		}
		if _, err := time.Parse(time.RFC3339Nano, r.CompletedAt); err != nil {
			return errors.New("invalid persisted recovery traffic execution")
		}
		r.OperationCompleted = true
	default:
		return errors.New("invalid persisted recovery traffic execution")
	}

	return nil
}
