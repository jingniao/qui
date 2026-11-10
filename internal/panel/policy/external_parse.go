package policy

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"

	shared "proxycontrol/pkg/contracts/policy"
	"gopkg.in/yaml.v3"
)

type ParsedEntries = shared.ParsedEntries

const MaxSourceEntries = 20000

var (
	ErrNotParsable = errors.New("rule format is client-only")
	ErrEmptyList   = errors.New("rule list is empty")
)

// ParseRuleList parses a fetched rule file. mrs is never parsed (returns ErrNotParsable).
func ParseRuleList(data []byte, format, behavior string) (ParsedEntries, int, error) {
	if format == "mrs" {
		return ParsedEntries{}, 0, ErrNotParsable
	}
	if format != "yaml" && format != "text" {
		return ParsedEntries{}, 0, errors.New("unsupported rule format")
	}
	if behavior != "classical" && behavior != "domain" && behavior != "ipcidr" {
		return ParsedEntries{}, 0, errors.New("unsupported rule behavior")
	}

	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var items []string
	if format == "yaml" {
		var doc struct {
			Payload []string `yaml:"payload"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return ParsedEntries{}, 0, err
		}
		items = doc.Payload
	} else {
		items = strings.Split(string(data), "\n")
	}

	var (
		domains      []string
		suffixes     []string
		keywords     []string
		cidrs        []string
		seenDomains  = make(map[string]bool)
		seenSuffixes = make(map[string]bool)
		seenKeywords = make(map[string]bool)
		seenCIDRs    = make(map[string]bool)
		skipped      int
	)

	total := func() int {
		return len(domains) + len(suffixes) + len(keywords) + len(cidrs)
	}

	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" || strings.HasPrefix(item, "#") || strings.HasPrefix(item, "//") {
			continue
		}

		switch behavior {
		case "classical":
			parts := strings.Split(item, ",")
			if len(parts) < 2 {
				skipped++
				continue
			}
			typeStr := strings.ToUpper(strings.TrimSpace(parts[0]))
			valStr := strings.TrimSpace(parts[1])

			switch typeStr {
			case "DOMAIN":
				d, err := shared.Domain(valStr, false)
				if err != nil {
					skipped++
					continue
				}
				if seenDomains[d] {
					continue
				}
				if total() >= MaxSourceEntries {
					skipped++
					continue
				}
				seenDomains[d] = true
				domains = append(domains, d)

			case "DOMAIN-SUFFIX":
				valStr = strings.TrimPrefix(valStr, ".")
				s, err := shared.Domain(valStr, false)
				if err != nil {
					skipped++
					continue
				}
				if seenSuffixes[s] {
					continue
				}
				if total() >= MaxSourceEntries {
					skipped++
					continue
				}
				seenSuffixes[s] = true
				suffixes = append(suffixes, s)

			case "DOMAIN-KEYWORD":
				k, err := shared.Keyword(valStr)
				if err != nil {
					skipped++
					continue
				}
				if seenKeywords[k] {
					continue
				}
				if total() >= MaxSourceEntries {
					skipped++
					continue
				}
				seenKeywords[k] = true
				keywords = append(keywords, k)

			case "IP-CIDR", "IP-CIDR6":
				var normalized string
				if prefix, err := netip.ParsePrefix(valStr); err == nil {
					normalized = prefix.Masked().String()
				} else if addr, err := netip.ParseAddr(valStr); err == nil {
					normalized = addr.String()
				} else {
					skipped++
					continue
				}
				if seenCIDRs[normalized] {
					continue
				}
				if total() >= MaxSourceEntries {
					skipped++
					continue
				}
				seenCIDRs[normalized] = true
				cidrs = append(cidrs, normalized)

			default:
				skipped++
			}

		case "domain":
			var (
				isSuffix bool
				val      string
			)
			if strings.HasPrefix(item, "+.") {
				isSuffix = true
				val = strings.TrimPrefix(item, "+.")
			} else if strings.HasPrefix(item, ".") {
				isSuffix = true
				val = strings.TrimPrefix(item, ".")
			} else {
				val = item
			}

			d, err := shared.Domain(val, false)
			if err != nil {
				skipped++
				continue
			}

			if isSuffix {
				if seenSuffixes[d] {
					continue
				}
				if total() >= MaxSourceEntries {
					skipped++
					continue
				}
				seenSuffixes[d] = true
				suffixes = append(suffixes, d)
			} else {
				if seenDomains[d] {
					continue
				}
				if total() >= MaxSourceEntries {
					skipped++
					continue
				}
				seenDomains[d] = true
				domains = append(domains, d)
			}

		case "ipcidr":
			var normalized string
			if prefix, err := netip.ParsePrefix(item); err == nil {
				normalized = prefix.Masked().String()
			} else if addr, err := netip.ParseAddr(item); err == nil {
				normalized = addr.String()
			} else {
				skipped++
				continue
			}
			if seenCIDRs[normalized] {
				continue
			}
			if total() >= MaxSourceEntries {
				skipped++
				continue
			}
			seenCIDRs[normalized] = true
			cidrs = append(cidrs, normalized)
		}
	}

	if total() == 0 {
		return ParsedEntries{}, skipped, ErrEmptyList
	}

	return ParsedEntries{
		Domains:  domains,
		Suffixes: suffixes,
		Keywords: keywords,
		CIDRs:    cidrs,
	}, skipped, nil
}
