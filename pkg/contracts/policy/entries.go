package policy

// ParsedEntries is the encrypted rule-source cache shared by the panel resolver
// and backup validation. It contains only normalized policy match entries.
type ParsedEntries struct {
	Domains  []string `json:"domains,omitempty"`
	Suffixes []string `json:"suffixes,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
	CIDRs    []string `json:"cidrs,omitempty"`
}
