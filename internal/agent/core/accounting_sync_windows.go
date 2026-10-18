package core

// Match the existing Windows node persistence boundary. Directory fsync is
// unavailable through this API; Windows power-loss durability is unverified.
func syncAccountingDirectory(string) error { return nil }
