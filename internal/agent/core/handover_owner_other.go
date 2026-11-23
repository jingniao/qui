//go:build !linux

package core

func handoverFileOwner(string) (int, int, error) { return -1, -1, nil }
