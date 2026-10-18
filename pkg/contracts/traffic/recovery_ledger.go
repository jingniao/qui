package traffic

type RecoveryBudget struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	Sequence int64  `json:"sequence"`
	Ceiling  int64  `json:"ceiling"`
	Spent    int64  `json:"spent"`
}
type RecoveryConnection struct {
	UserID     string `json:"userId"`
	Historical bool   `json:"historical"`
}
type RecoveryLedgerEntries struct {
	Budgets      []RecoveryBudget     `json:"budgets"`
	Connections  []RecoveryConnection `json:"connections"`
	LimitedUsers []string             `json:"limitedUsers"`
}

type RecoverySource struct {
	Epoch        string `json:"epoch"`
	Sequence     int64  `json:"sequence"`
	Revision     int64  `json:"revision"`
	Digest       string `json:"digest"`
	ClosedDigest string `json:"closedDigest"`
	Acknowledged bool   `json:"acknowledged"`
	Precise      bool   `json:"precise"`
}
type RecoveryFinal struct {
	Epoch    string `json:"epoch"`
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
}
type RecoveryLifecycle struct {
	Cursors       []RecoveryCursor          `json:"cursors,omitempty"`
	Sources       []RecoverySource          `json:"sources"`
	PendingFinals []RecoveryFinal           `json:"pendingFinals"`
	Retirements   []BudgetRetirementReceipt `json:"retirements"`
	Protection    *RecoveryProtection       `json:"protection"`
}
type RecoveryCursor struct {
	Epoch         string `json:"epoch"`
	CredentialID  string `json:"credentialId"`
	Upload        int64  `json:"upload"`
	Download      int64  `json:"download"`
	IdentityKnown bool   `json:"identityKnown"`
	UserID        string `json:"userId"`
	DeviceID      string `json:"deviceId"`
	Revision      int64  `json:"revision"`
}

type RecoveryProtection struct {
	JournalPresent  bool                          `json:"journalPresent"`
	Epoch           string                        `json:"epoch"`
	Revision        int64                         `json:"revision"`
	Stage           string                        `json:"stage"`
	PendingCounters int                           `json:"pendingCounters"`
	LocalCompletion *RecoveryAccountingCompletion `json:"localCompletion,omitempty"`
}

// One physical original journal's native-frozen local completion only. It
// does not attest to central debits, ACK delivery or all historical coverage.
type RecoveryAccountingCompletion struct {
	Epoch                string `json:"epoch"`
	Revision             int64  `json:"revision"`
	JournalDigest        string `json:"journalDigest"`
	FinalDigest          string `json:"finalDigest"`
	HistoryAuthenticated bool   `json:"historyAuthenticated"`
}
