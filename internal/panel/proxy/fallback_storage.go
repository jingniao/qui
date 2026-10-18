package proxy

// New modes use the additive column; the legacy column keeps a valid, safe
// value. Previous migrations and persisted snapshots remain immutable.
func legacyFailure(mode string, chain bool) string {
	if mode == "Block" || mode == "Direct" || mode == "BackupOutbound" {
		return mode
	}
	if chain && mode == "BackupChain" {
		return mode
	}
	return "Block"
}
