package agentpb

// ReplacementDrain carries the hash of an issued recovery token on a stopped
// Agent's mTLS stream. The panel acknowledges this mode in the header and only
// confirms completion in the successful closing trailer.
const ReplacementDrain = "qui-replacement-drain"
