package desiredstate

import "errors"

var ErrKeywordUnsupported = errors.New("keyword_unsupported")

type CoreKeywordCapability struct {
	Name                   string
	DomainKeywordSupported bool
}

// RequireKeywordSupport fails when any enabled policy uses keywords and any enabled
// xray/sing-box core in cores lacks DomainKeywordSupported.
func RequireKeywordSupport(state State, cores []CoreKeywordCapability) error {
	hasKeyword := false
	for _, policy := range state.Policies {
		if policy.Enabled && len(policy.Match.Keywords) > 0 {
			hasKeyword = true
			break
		}
	}
	if !hasKeyword {
		return nil
	}

	for _, core := range cores {
		if core.Name == CoreGost {
			continue
		}
		if !core.DomainKeywordSupported {
			return ErrKeywordUnsupported
		}
	}
	return nil
}
