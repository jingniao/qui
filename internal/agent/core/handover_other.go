//go:build !linux

package core

func HandoverBoottimeNS() (int64, error)                        { return 0, ErrHandoverUnsupported }
func checkHandoverUpdater(ProcessIdentity, OSEnvironment) error { return ErrHandoverUnsupported }
func checkRequestPermissions() error                            { return ErrHandoverUnsupported }
func prepareRequestDirectory() (int, error)                     { return 0, ErrHandoverUnsupported }
