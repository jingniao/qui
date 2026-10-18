package traffic

type RecoveryAccountingObservation struct {
	CaseID                     string                        `json:"caseId"`
	NodeID                     string                        `json:"nodeId"`
	SnapshotDigest             string                        `json:"snapshotDigest"`
	EvidenceSHA256             string                        `json:"evidenceSha256"`
	ObservedAt                 string                        `json:"observedAt"`
	Scope                      string                        `json:"scope"`
	Status                     string                        `json:"status"`
	Message                    string                        `json:"message"`
	OriginalEvidenceReverified bool                          `json:"originalEvidenceReverified"`
	LocalCompletion            *RecoveryAccountingCompletion `json:"localCompletion,omitempty"`
	CurrentChecked             bool                          `json:"currentChecked"`
	CurrentClosureObserved     bool                          `json:"currentClosureObserved"`
	AccountsConfirmed          *bool                         `json:"accountsConfirmed,omitempty"`
	Settled                    bool                          `json:"settled"`
	RecoveryPermission         bool                          `json:"recoveryPermission"`
}
