package importer

import (
	"proxycontrol/internal/panel/subscription/proxy"
)

// Parse reports a generic diagnostic without echoing untrusted input, which may
// include a password, a token or the source's private subscription URL. It is
// Analyze without the report, kept for the format.Parser contract.
func Parse(b []byte) ([]proxy.Proxy, []proxy.Warning, error) {
	result, entries := Analyze(b)
	if result.Outcome == "failed" || len(entries) == 0 {
		return nil, nil, ErrParse
	}
	return entries, []proxy.Warning{proxy.SourceWarning("imported", "Only supported valid proxy entries are imported; unsupported fields and entries are omitted.")}, nil
}
