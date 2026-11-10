package server

import (
	"context"
	"database/sql"
	"errors"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/subscription/rules"
)

// ruleLookup answers the client rule template's reference checks from the
// node and policy modules, which own those objects.
type ruleLookup struct {
	nodes    *node.Repository
	policies *policy.Repository
}

func (l ruleLookup) ServiceSetExists(ctx context.Context, id string, requireEnabled bool) bool {
	service, err := l.policies.Service(ctx, id)
	return err == nil && (!requireEnabled || service.Enabled)
}

func (l ruleLookup) NodeGroupExists(ctx context.Context, id string) bool {
	_, err := l.nodes.Group(ctx, id)
	return err == nil
}

func (l ruleLookup) NodeExists(ctx context.Context, id string) bool {
	n, err := l.nodes.Get(ctx, id)
	return err == nil && n.DeletedAt == ""
}

// serviceRules serves panel service rules to the subscription module.
type serviceRules struct {
	policies *policy.Repository
}

func (s serviceRules) ServiceRule(ctx context.Context, id string) (subscription.ServiceRule, error) {
	service, err := s.policies.Service(ctx, id)
	if errors.Is(err, policy.ErrNotFound) {
		return subscription.ServiceRule{}, subscription.ErrServiceRuleNotFound
	}
	if err != nil {
		return subscription.ServiceRule{}, err
	}
	return subscription.ServiceRule{Enabled: service.Enabled, Domains: service.Domains, Suffixes: service.Suffixes, Keywords: service.Keywords, CIDRs: service.CIDRs}, nil
}

type clientServiceReferences struct{}

func (clientServiceReferences) ServiceSetReferences(ctx context.Context, tx *sql.Tx, id string) ([]policy.ClientProfileReference, error) {
	profiles, err := rules.ServiceSetReferences(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	refs := []policy.ClientProfileReference{}
	for _, profile := range profiles {
		ref := policy.ClientProfileReference{ID: profile.ID, Name: profile.Name, Active: profile.Active, Rules: []policy.RuleReference{}}
		for _, rule := range profile.Rules {
			ref.Rules = append(ref.Rules, policy.RuleReference{ID: rule.ID, Enabled: rule.Enabled})
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
