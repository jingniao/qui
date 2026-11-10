package desiredstate_test

import (
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

func TestRequireKeywordSupport(t *testing.T) {
	stateWithoutKeywords := desiredstate.State{
		Policies: []desiredstate.Policy{
			{
				Enabled: true,
				Match:   desiredstate.Match{Domains: []string{"example.com"}},
			},
		},
	}
	stateWithKeywords := desiredstate.State{
		Policies: []desiredstate.Policy{
			{
				Enabled: true,
				Match:   desiredstate.Match{Keywords: []string{"openai"}},
			},
		},
	}
	disabledKeywordState := desiredstate.State{
		Policies: []desiredstate.Policy{
			{
				Enabled: false,
				Match:   desiredstate.Match{Keywords: []string{"openai"}},
			},
		},
	}

	// 1. 无关键字放行
	oldCore := desiredstate.CoreKeywordCapability{Name: "xray", DomainKeywordSupported: false}
	if err := desiredstate.RequireKeywordSupport(stateWithoutKeywords, []desiredstate.CoreKeywordCapability{oldCore}); err != nil {
		t.Fatalf("expected pass without keywords, got: %v", err)
	}

	// 2. 停用策略带关键字放行
	if err := desiredstate.RequireKeywordSupport(disabledKeywordState, []desiredstate.CoreKeywordCapability{oldCore}); err != nil {
		t.Fatalf("expected pass for disabled keyword policy, got: %v", err)
	}

	// 3. 全部核心支持放行
	modernXray := desiredstate.CoreKeywordCapability{Name: "xray", DomainKeywordSupported: true}
	modernSingbox := desiredstate.CoreKeywordCapability{Name: "sing-box", DomainKeywordSupported: true}
	if err := desiredstate.RequireKeywordSupport(stateWithKeywords, []desiredstate.CoreKeywordCapability{modernXray, modernSingbox}); err != nil {
		t.Fatalf("expected pass with modern cores, got: %v", err)
	}

	// 4. 任一核心不支持拒绝
	if err := desiredstate.RequireKeywordSupport(stateWithKeywords, []desiredstate.CoreKeywordCapability{modernXray, oldCore}); err != desiredstate.ErrKeywordUnsupported {
		t.Fatalf("expected ErrKeywordUnsupported, got: %v", err)
	}

	// 5. GOST 不参与（即使 DomainKeywordSupported=false 也放行）
	gostCore := desiredstate.CoreKeywordCapability{Name: desiredstate.CoreGost, DomainKeywordSupported: false}
	if err := desiredstate.RequireKeywordSupport(stateWithKeywords, []desiredstate.CoreKeywordCapability{modernXray, gostCore}); err != nil {
		t.Fatalf("expected GOST core to not participate, got: %v", err)
	}
}
