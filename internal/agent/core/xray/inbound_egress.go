package xray

import (
	"strconv"
	"time"

	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

func applyInboundEgress(doc *configDocument, state, original desiredstate.State, credentials map[string]desiredstate.Credential, at time.Time, options RenderOptions, aliases map[string]string, probes []core.Probe, rendered *core.RenderedConfig) error {
	sources := core.InboundEgressSources(state)
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
		scopedOriginal := original
		scopedOriginal.Inbounds = scopedState.Inbounds
		var scoped configDocument
		if options.Access {
			scoped.accessOrigins = map[string]collector.Exit{}
		}
		sourceOptions := options
		sourceOptions.Health = core.SourceHealth(state, options.Health, source)
		if err := compileRouting(&scoped, scopedState, scopedOriginal, credentials, at, sourceOptions, aliases); err != nil {
			return err
		}
		for i := range scoped.Routing.Rules {
			if len(scoped.Routing.Rules[i].InboundTag) == 0 {
				scoped.Routing.Rules[i].InboundTag = inboundTags
			}
		}
		suffix := "~egress~" + strconv.Itoa(index)
		addProbeInbounds(&scoped, probes, source, suffix, options, rendered)
		scopedRules = append(scopedRules, mergeEgress(doc, &scoped, source, suffix)...)
	}
	doc.Routing.Rules = append(scopedRules, doc.Routing.Rules...)
	if doc.Observatory != nil {
		doc.Observatory.SubjectSelector = nil
		for _, bal := range doc.Routing.Balancers {
			doc.Observatory.SubjectSelector = append(doc.Observatory.SubjectSelector, bal.Selector...)
		}
	}
	return nil
}

// Merge only outbounds and balancers reachable from a source's public or
// probe routes. Each graph owns native stream settings and access metadata.
func mergeEgress(doc, scoped *configDocument, source, suffix string) []route {
	outTags := map[string]string{}
	balancers := map[string]bool{}
	for i := range scoped.Routing.Rules {
		rule := &scoped.Routing.Rules[i]
		if rule.BalancerTag != "" {
			balancers[rule.BalancerTag] = true
			rule.BalancerTag += suffix
		} else {
			outTags[rule.OutboundTag] = rule.OutboundTag + suffix
		}
	}
	firstBalancer := len(doc.Routing.Balancers)
	for _, bal := range scoped.Routing.Balancers {
		if !balancers[bal.Tag] {
			continue
		}
		bal.Tag += suffix
		for i, tag := range bal.Selector {
			// Native selectors match prefixes. A source member must not start
			// with the corresponding default member's tag, even under access logging.
			outTags[tag] = "_access_egress_selector" + suffix + "_" + tag
			bal.Selector[i] = outTags[tag]
		}
		outTags[bal.FallbackTag] = bal.FallbackTag + suffix
		doc.Routing.Balancers = append(doc.Routing.Balancers, bal)
		if doc.Observatory == nil {
			settings := *scoped.Observatory
			doc.Observatory = &settings
		}
	}
	for _, out := range scoped.Outbounds {
		tag, used := outTags[out.Tag]
		if !used {
			continue
		}
		if out.Protocol == "blackhole" && out.Tag == out.sourceID {
			outTags[out.Tag] = out.Tag
			continue
		}
		if doc.accessOrigins != nil {
			doc.accessOrigins[tag] = scoped.accessOrigins[out.Tag]
		}
		out.Tag, out.egressSourceIP = tag, source
		doc.Outbounds = append(doc.Outbounds, out)
	}
	for i := range scoped.Routing.Rules {
		rule := &scoped.Routing.Rules[i]
		if rule.OutboundTag != "" {
			rule.OutboundTag = outTags[rule.OutboundTag]
		}
	}
	for i := firstBalancer; i < len(doc.Routing.Balancers); i++ {
		bal := &doc.Routing.Balancers[i]
		bal.FallbackTag = outTags[bal.FallbackTag]
	}
	doc.Inbounds = append(doc.Inbounds, scoped.Inbounds...)
	return scoped.Routing.Rules
}
