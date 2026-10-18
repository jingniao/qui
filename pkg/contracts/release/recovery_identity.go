package release

// RecoveryIdentity contains metadata, public signed proofs and public CA
// evidence only, never private keys, credentials or permission to reopen traffic.
type RecoveryIdentity struct {
	FormatFloorPresent     bool                 `json:"formatFloorPresent"`
	IdentityFormat         int                  `json:"identityFormat"`
	Material               string               `json:"material"`
	CertificateFingerprint string               `json:"certificateFingerprint"`
	TrustedCAFingerprints  []string             `json:"trustedCAFingerprints"`
	PlanDigest             string               `json:"planDigest,omitempty"`
	SuccessorOnly          bool                 `json:"successorOnly"`
	RetirementProof        []byte               `json:"retirementProof,omitempty"`
	PendingKeyRotation     bool                 `json:"pendingKeyRotation"`
	NextCA                 *RecoveryNextCA      `json:"nextCA,omitempty"`
	FollowingCA            *RecoveryFollowingCA `json:"followingCA,omitempty"`
	RoundCA                *RecoveryRoundCA     `json:"roundCA,omitempty"`
	LongCA                 *RecoveryLongCA      `json:"longCA,omitempty"`
	RecoveredTrust         *RecoveryOwnerTrust  `json:"recoveredTrust,omitempty"`
}

// Explicit Owner root approval is distinct from an ordinary CA round proof.
// Public certificates, signed authorization and local ciphertext digests only.
type RecoveryOwnerTrust struct {
	RequestKey            string `json:"requestKey"`
	PreparedRecordDigest  string `json:"preparedRecordDigest"`
	HeadRecordDigest      string `json:"headRecordDigest"`
	ApprovedCAFingerprint string `json:"approvedCaFingerprint"`
	OriginalFormat        int    `json:"originalFormat"`
	OriginalAck           string `json:"originalAck"`
	PreviousCertificate   []byte `json:"previousCertificate"`
	IssuedCertificate     []byte `json:"issuedCertificate"`
	PreviousCA            []byte `json:"previousCA"`
	ApprovedCA            []byte `json:"approvedCA"`
	AuthorizationProof    []byte `json:"authorizationProof"`
}

// Public evidence only. The source format selects the retained anchor branch;
// no private key, encrypted configuration or activation permission is included.
type RecoveryNextCA struct {
	Retired                      bool              `json:"retired,omitempty"`
	RetirementRecordPresent      bool              `json:"retirementRecordPresent,omitempty"`
	RetirementProof              []byte            `json:"retirementProof,omitempty"`
	Active                       bool              `json:"active"`
	ActivationRecordPresent      bool              `json:"activationRecordPresent"`
	SourceFormat                 int               `json:"sourceFormat"`
	PlanDigest                   string            `json:"planDigest"`
	Manifest                     []byte            `json:"manifest"`
	Files                        map[string][]byte `json:"files"`
	StagedAt                     string            `json:"stagedAt"`
	ActivatedAt                  string            `json:"activatedAt,omitempty"`
	StagedCertificateFingerprint string            `json:"stagedCertificateFingerprint"`
	ParentRetirementProof        []byte            `json:"parentRetirementProof,omitempty"`
}

// Digests describe retained local ciphertext, never encrypted content or keys.
type RecoveryFollowingCA struct {
	Stage                       *RecoveryNextCA `json:"stage"`
	ParentLocalRetirementDigest string          `json:"parentLocalRetirementDigest"`
	StageRecordDigest           string          `json:"stageRecordDigest"`
	ActiveRecordDigest          string          `json:"activeRecordDigest,omitempty"`
	RetirementRecordDigest      string          `json:"retirementRecordDigest,omitempty"`
}

// ParentIdentity is public historical evidence, not the current runtime roots.
// The first plan-addressed generation must have a format9 parent and no nested
// RoundCA; later-generation support must explicitly validate its history.
type RecoveryRoundCA struct {
	Generation                  uint64            `json:"generation"`
	Stage                       *RecoveryNextCA   `json:"stage"`
	ParentIdentity              *RecoveryIdentity `json:"parentIdentity"`
	ParentLocalRetirementDigest string            `json:"parentLocalRetirementDigest"`
	StageRecordDigest           string            `json:"stageRecordDigest"`
	ActiveRecordDigest          string            `json:"activeRecordDigest,omitempty"`
	HeadRecordDigest            string            `json:"headRecordDigest,omitempty"`
	HeadPhase                   string            `json:"headPhase,omitempty"`
	RetirementRecordDigest      string            `json:"retirementRecordDigest,omitempty"`
	PreviousHeadDigest          string            `json:"previousHeadDigest,omitempty"`
	ParentClosedHeadDigest      string            `json:"parentClosedHeadDigest,omitempty"`
	ClosedRecordDigest          string            `json:"closedRecordDigest,omitempty"`
}

// Flat oldest-first closed history. It does not replace current runtime roots.
type RecoveryLongCA struct {
	Base    *RecoveryIdentity `json:"base"`
	History []RecoveryRoundCA `json:"history"`
	Current *RecoveryRoundCA  `json:"current"`
	Pending *RecoveryRoundCA  `json:"pending,omitempty"`
}
