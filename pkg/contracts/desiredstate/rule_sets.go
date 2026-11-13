package desiredstate

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// RuleSet is a node-native rule set file a policy matches against (v1.5.4 item
// 5): one list in the two encodings the cores read directly. sing-box loads the
// binary rule set at SingBoxURL as a local rule_set; Xray loads tag XrayTag
// from the geosite (domain) or geoip (ipcidr) data file at XrayURL. The Agent
// downloads both files itself, keeps the last good copy, and refreshes a copy
// older than RefreshSeconds when it applies a revision; the cores never fetch.
// A policy carrying rule sets is sent only to Agents whose cores report
// NativeRuleSetsSupported (RequireNativeRuleSetSupport).
type RuleSet struct {
	// ID is the stable source ID; renderers derive their tags from it.
	ID             string `json:"id"`
	Behavior       string `json:"behavior"`
	SingBoxURL     string `json:"sing_box_url"`
	XrayURL        string `json:"xray_url"`
	XrayTag        string `json:"xray_tag"`
	RefreshSeconds int    `json:"refresh_seconds"`
}

// MaxPolicyRuleSets bounds the native rule sets of one policy.
const MaxPolicyRuleSets = 16

// MaxRuleSetFileBytes bounds one downloaded rule set file.
const MaxRuleSetFileBytes = 32 << 20

// RuleSetHosts are the only hosts rule set files are fetched from. Both the
// panel (on save) and the Agent (before every download, and for every redirect)
// check them.
var RuleSetHosts = []string{"testingcf.jsdelivr.net", "cdn.jsdelivr.net", "fastly.jsdelivr.net"}

var (
	ruleSetIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
	ruleSetTagPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9!@._-]{0,63}$`)
	singBoxPathPattern = regexp.MustCompile(`^/gh/MetaCubeX/meta-rules-dat@sing/geo/(geosite|geoip)/[A-Za-z0-9!@._-]{1,96}\.srs$`)
	xrayPathPattern    = regexp.MustCompile(`^/gh/MetaCubeX/meta-rules-dat@release/(geosite|geoip)\.dat$`)
)

var ErrNativeRuleSetUnsupported = errors.New("native_rule_set_unsupported")

// RuleSetURLAllowed reports whether address is an HTTPS link on a RuleSetHosts
// host whose path is a MetaCubeX meta-rules-dat rule set file: a sing-box .srs
// (singBox) or an Xray geosite/geoip .dat. Query, fragment, user info and
// explicit ports are refused.
func RuleSetURLAllowed(address string, singBox bool) bool {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.Port() != "" || strings.Contains(parsed.Path, "..") {
		return false
	}
	if !RuleSetHostAllowed(parsed.Hostname()) {
		return false
	}
	if singBox {
		return singBoxPathPattern.MatchString(parsed.EscapedPath())
	}
	return xrayPathPattern.MatchString(parsed.EscapedPath())
}

// RuleSetHostAllowed reports whether host is one of RuleSetHosts.
func RuleSetHostAllowed(host string) bool {
	for _, allowed := range RuleSetHosts {
		if strings.EqualFold(host, allowed) {
			return true
		}
	}
	return false
}

// ValidateRuleSet checks one reference. The Xray file must be the geosite file
// for a domain list and the geoip file for an IP list.
func ValidateRuleSet(set RuleSet) error {
	if !ruleSetIDPattern.MatchString(set.ID) || !ruleSetTagPattern.MatchString(set.XrayTag) || set.RefreshSeconds < 3600 || set.RefreshSeconds > 604800 {
		return errors.New("invalid native rule set")
	}
	kind := "geosite"
	switch set.Behavior {
	case "domain":
	case "ipcidr":
		kind = "geoip"
	default:
		return errors.New("invalid native rule set behavior")
	}
	if !RuleSetURLAllowed(set.SingBoxURL, true) || !RuleSetURLAllowed(set.XrayURL, false) {
		return errors.New("native rule set URL not allowed")
	}
	if !strings.Contains(set.SingBoxURL, "/geo/"+kind+"/") || !strings.HasSuffix(set.XrayURL, "/"+kind+".dat") {
		return errors.New("native rule set files do not match its behavior")
	}
	return nil
}

// RequireNativeRuleSetSupport fails when an enabled policy carries rule sets and
// any enabled xray/sing-box core lacks NativeRuleSetsSupported. GOST renders no
// policies.
func RequireNativeRuleSetSupport(state State, cores []CoreKeywordCapability) error {
	for _, policy := range state.Policies {
		if !policy.Enabled || len(policy.Match.RuleSets) == 0 {
			continue
		}
		for _, core := range cores {
			if core.Name != CoreGost && !core.NativeRuleSetsSupported {
				return ErrNativeRuleSetUnsupported
			}
		}
	}
	return nil
}
