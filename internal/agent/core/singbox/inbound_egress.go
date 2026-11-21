package singbox

import (
	"strconv"
	"time"

	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

func egressSuffix(index int) string {
	return "~egress~" + strconv.Itoa(index)
}

func applyInboundEgress(doc *document, state, original desiredstate.State, credentials map[string]desiredstate.Credential, at time.Time, options renderOptions, aliases map[string]string, sources []string) error {
	if len(sources) == 0 {
		return nil
	}
	var scopedRules []route
	for index, source := range sources {
		scopedState := state
		scopedState.Inbounds = nil
		var inboundTags []string
		for _, in := range state.Inbounds {
			if in.Enabled && in.EgressSourceIP == source {
				scopedState.Inbounds = append(scopedState.Inbounds, in)
				inboundTags = append(inboundTags, in.ID)
			}
		}

		// Compile from the original members. By the time default routes exist,
		// their groups have already discarded failed or slower default paths.
		var scoped document
		scopedOptions := options
		scopedOptions.health = core.SourceHealth(state, options.health, source)
		scopedOptions.access = false // The default document already has the policy metadata tags.
		if err := compileOutbounds(&scoped, scopedState, credentials, scopedOptions); err != nil {
			return err
		}
		if err := compileRoutes(&scoped, scopedState, original, credentials, at, scopedOptions, aliases); err != nil {
			return err
		}

		suffix := egressSuffix(index)
		needed := make(map[string]bool)
		// Failed and unknown paths still need real, source-bound probe targets.
		for _, group := range state.OutboundGroups {
			for _, member := range group.Members {
				needed[member.OutboundID] = true
			}
		}
		for _, rule := range scoped.Route.Rules {
			if len(rule.Inbound) == 0 {
				rule.Inbound = inboundTags
			}
			if rule.Outbound != "" {
				needed[rule.Outbound] = true
				rule.Outbound += suffix
				rule.origin.NativeTag = rule.Outbound
			}
			if rule.candidates != nil {
				candidates := make(map[string]collector.Exit, len(rule.candidates))
				for tag, origin := range rule.candidates {
					origin.NativeTag = tag + suffix
					candidates[origin.NativeTag] = origin
				}
				rule.candidates = candidates
			}
			scopedRules = append(scopedRules, rule)
		}
		for _, out := range scoped.Outbounds {
			if needed[out.Tag] {
				out.Tag += suffix
				out.egressSourceIP = source
				for i := range out.Outbounds {
					out.Outbounds[i] += suffix
				}
				doc.Outbounds = append(doc.Outbounds, out)
			}
		}
		for _, endpoint := range scoped.Endpoints {
			if needed[endpoint.Tag] {
				endpoint.Tag += suffix
				endpoint.egressSourceIP = source
				doc.Endpoints = append(doc.Endpoints, endpoint)
			}
		}
	}
	doc.Route.Rules = append(scopedRules, doc.Route.Rules...)
	return nil
}
