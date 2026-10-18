package proxy

import (
	"context"
	"errors"
)

type PolicyReferences interface {
	UsesProxyTarget(context.Context, string) (bool, error)
}

func (s *Service) checkPolicyReference(ctx context.Context, targetID string) error {
	reader, ok := s.Policies.(PolicyReferences)
	if !ok {
		return nil
	}
	used, err := reader.UsesProxyTarget(ctx, targetID)
	if err != nil {
		return err
	}
	if used {
		return errors.New("proxy target is referenced by a policy")
	}
	return nil
}

func (s *Service) DeleteGroup(ctx context.Context, groupID string) error {
	if err := s.checkPolicyReference(ctx, groupID); err != nil {
		return err
	}
	return s.Repository.DeleteGroup(ctx, groupID)
}

func (s *Service) DeleteChain(ctx context.Context, chainID string) error {
	if err := s.checkPolicyReference(ctx, chainID); err != nil {
		return err
	}
	return s.Repository.DeleteChain(ctx, chainID)
}

func (s *Service) DeleteOutbound(ctx context.Context, outboundID string) error {
	if err := s.checkPolicyReference(ctx, outboundID); err != nil {
		return err
	}
	return s.Repository.DeleteOutbound(ctx, outboundID)
}
