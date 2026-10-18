//go:build windows

package agentjobs

import "os"

func syncDirectory(*os.Root) error { return nil }
