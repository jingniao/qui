package desiredstate

import (
	"errors"
	"fmt"
)

// MaxPolicyMatchers bounds the domain, suffix, keyword and CIDR entries of one
// policy as Agents before v1.5.2 accept it. MaxExpandedPolicyMatchers bounds a
// policy after its service rules and their external sources were expanded into
// it, for cores that report ExpandedPolicySupported.
const (
	MaxPolicyMatchers         = 2000
	MaxExpandedPolicyMatchers = 50000
)

// ErrExpandedPolicyUnsupported is returned when a policy expands to more than
// MaxPolicyMatchers entries and a core that renders policies does not accept
// that.
var ErrExpandedPolicyUnsupported = errors.New("expanded_policy_unsupported")

// ExpandedPolicyError names the policy and its expanded size.
type ExpandedPolicyError struct {
	PolicyName string
	Count      int
}

func (e *ExpandedPolicyError) Error() string {
	return fmt.Sprintf("%s: 策略「%s」展开规则集后有 %d 条匹配，超过该服务器 Agent 支持的 %d 条，请先升级 Agent", ErrExpandedPolicyUnsupported, e.PolicyName, e.Count, MaxPolicyMatchers)
}
func (e *ExpandedPolicyError) Unwrap() error { return ErrExpandedPolicyUnsupported }

// RequireExpandedPolicySupport fails when an enabled policy carries more than
// MaxPolicyMatchers entries and any enabled xray/sing-box core lacks
// ExpandedPolicySupported. GOST renders no policies.
func RequireExpandedPolicySupport(state State, cores []CoreKeywordCapability) error {
	for _, policy := range state.Policies {
		count := len(policy.Match.Domains) + len(policy.Match.Suffixes) + len(policy.Match.Keywords) + len(policy.Match.CIDRs)
		if !policy.Enabled || count <= MaxPolicyMatchers {
			continue
		}
		for _, core := range cores {
			if core.Name != CoreGost && !core.ExpandedPolicySupported {
				return &ExpandedPolicyError{PolicyName: policy.Name, Count: count}
			}
		}
	}
	return nil
}
