//go:build !linux

package capability

func diskBytes(string) (*uint64, *uint64) { return nil, nil }
