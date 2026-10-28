package server

import (
	"context"
	"errors"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/subscription"
)

// ruleLookup answers the client rule template's reference checks from the
// node and policy modules, which own those objects.
type ruleLookup struct {
	nodes    *node.Repository
	policies *policy.Repository
}

func (l ruleLookup) ServiceSetExists(ctx context.Context, id string) bool {
	_, err := l.policies.Service(ctx, id)
	return err == nil
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
	return subscription.ServiceRule{Enabled: service.Enabled, Domains: service.Domains, Suffixes: service.Suffixes, CIDRs: service.CIDRs}, nil
}
