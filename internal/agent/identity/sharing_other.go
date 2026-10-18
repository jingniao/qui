//go:build !windows

package identity

func sharingConflict(error) bool { return false }
