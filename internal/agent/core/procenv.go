package core

import (
	"os"
	"strings"
)

// CoreEnvironment is the environment every proxy core is started with: the
// Agent's own, with multipathtcp=0 in GODEBUG. Go 1.24 and later open listening
// sockets as MPTCP by default on Linux, and the remote root exploit for the
// kernel bug CVE-2026-98432 needs an MPTCP listener. Xray and sing-box are Go
// programs, so without this every proxy inbound would listen with MPTCP. A
// multipathtcp value already in GODEBUG is replaced; other settings are kept.
func CoreEnvironment() []string {
	return coreEnvironment(os.Environ())
}

func coreEnvironment(parent []string) []string {
	environment := make([]string, 0, len(parent)+1)
	godebug := []string{"multipathtcp=0"}
	for _, entry := range parent {
		value, ok := strings.CutPrefix(entry, "GODEBUG=")
		if !ok {
			environment = append(environment, entry)
			continue
		}
		for _, setting := range strings.Split(value, ",") {
			if setting != "" && !strings.HasPrefix(setting, "multipathtcp=") {
				godebug = append(godebug, setting)
			}
		}
	}
	return append(environment, "GODEBUG="+strings.Join(godebug, ","))
}
