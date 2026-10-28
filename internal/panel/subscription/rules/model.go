// Package rules holds the global client rule template: the Clash-style
// settings, proxy groups, rule providers and rules every user's full profile is
// built from (plan section 4.4).
//
// The template is neutral: it names group members symbolically (@all,
// @nodeGroup:<id>) and is expanded per user against that user's own proxies, so
// one template gives every user groups that contain exactly their own nodes.
// Exporters map the expanded rules through their own capability tables; this
// package never renders a client format.
//
// Client rules never replace node policies: they only choose, on the user's
// device, between going direct, rejecting and which proxy group to use.
package rules

// Template is the stored rule template.
type Template struct {
	// Preset names the preset the template was last built from; it is a label
	// for the page, not a live link.
	Preset    string     `json:"preset"`
	Client    Client     `json:"client"`
	DNS       DNS        `json:"dns"`
	Groups    []Group    `json:"groups"`
	Providers []Provider `json:"providers"`
	Rules     []Rule     `json:"rules"`
}

// Client is the top level of a Clash profile.
type Client struct {
	Mode      string `json:"mode"`
	MixedPort int    `json:"mixedPort"`
	AllowLAN  bool   `json:"allowLan"`
	LogLevel  string `json:"logLevel"`
	IPv6      bool   `json:"ipv6"`
}

// DNS is the client DNS section. It is off by default: the template does not
// pick resolvers for the Owner.
type DNS struct {
	Enable       bool     `json:"enable"`
	EnhancedMode string   `json:"enhancedMode"`
	Nameservers  []string `json:"nameservers"`
	Fallback     []string `json:"fallback"`
}

// Group is a client proxy group. A member is DIRECT, REJECT, another group's
// name, @all (every proxy of the user) or @nodeGroup:<id> (the user's proxies
// on that granted node group). @source:<id> expands only that user's granted
// imported proxies; it cannot grant access by itself.
type Group struct {
	Name            string   `json:"name"`
	Type            string   `json:"type"`
	Members         []string `json:"members"`
	TestURL         string   `json:"testUrl"`
	IntervalSeconds int      `json:"intervalSeconds"`
}

// Provider is an external rule set the client fetches itself.
type Provider struct {
	Name     string `json:"name"`
	Behavior string `json:"behavior"`
	// Format is the list format: yaml (the default when empty), text or mrs
	// (binary, domain and ipcidr only).
	Format          string `json:"format"`
	URL             string `json:"url"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

// Rule is one client rule. Rules run in ascending priority; the last enabled
// rule must be the only MATCH. A RULE-SET payload names a provider, or a panel
// service rule as service:<id>, which the panel serves to the client as a rule
// provider. A target is DIRECT, REJECT, a group name or node:<id> (the user's
// proxy on that node).
type Rule struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Payload     string `json:"payload"`
	Target      string `json:"target"`
	NoResolve   bool   `json:"noResolve"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
	Description string `json:"description"`
}

// The rule types the template accepts. Exporters that cannot express a type
// drop the rule with a warning; they never rewrite its target.
const (
	RuleDomain        = "DOMAIN"
	RuleDomainSuffix  = "DOMAIN-SUFFIX"
	RuleDomainKeyword = "DOMAIN-KEYWORD"
	RuleIPCIDR        = "IP-CIDR"
	RuleIPCIDR6       = "IP-CIDR6"
	RuleGeoIP         = "GEOIP"
	RuleGeoSite       = "GEOSITE"
	RuleDstPort       = "DST-PORT"
	RuleSet           = "RULE-SET"
	RuleMatch         = "MATCH"
)

// Built-in targets and member keywords.
const (
	TargetDirect = "DIRECT"
	TargetReject = "REJECT"

	MemberAll        = "@all"
	MemberNodeGroup  = "@nodeGroup:"
	MemberSource     = "@source:"
	TargetNodePrefix = "node:"
	ServicePrefix    = "service:"
)

// Limits of one template (plan section 4.4).
const (
	MaxGroups       = 64
	MaxRules        = 2000
	MaxProviders    = 32
	MaxMembers      = 256
	MaxNameRunes    = 64
	MaxPayloadBytes = 256
	MaxDescription  = 120
	MaxResolvers    = 16
	MaxRuleIDBytes  = 64
)
