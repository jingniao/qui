package traffic

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

// One physical original journal's native-frozen local completion only. It
// does not attest to central debits, ACK delivery or all historical coverage.
type RecoveryAccountingCompletion struct {
	Epoch                string `json:"epoch"`
	Revision             int64  `json:"revision"`
	JournalDigest        string `json:"journalDigest"`
	FinalDigest          string `json:"finalDigest"`
	HistoryAuthenticated bool   `json:"historyAuthenticated"`
}
