package proxy

import (
	"context"
	"database/sql"
)

func rejectSchemeEntryInbound(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, inboundID string) error {
	var owned bool
	if err := query.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM canvas_entry_bindings WHERE inbound_id=?)", inboundID).Scan(&owned); err != nil {
		return err
	}
	if owned {
		return tunnelFail("inbound_is_scheme_entry", 400, "该入站是方案入口，不能作为方案的出口、中转实例或隧道规则的关联目标")
	}
	return nil
}

func (s *Service) validateCanvasEntryOwnership(ctx context.Context, layout CanvasLayout) error {
	for _, instance := range layout.Instances {
		ref, err := parseCanvasRef(instance.Ref)
		if err != nil || ref.kind != "inbound" {
			continue
		}
		if err := rejectSchemeEntryInbound(ctx, s.Repository.Database, ref.id); err != nil {
			return err
		}
	}
	return nil
}
