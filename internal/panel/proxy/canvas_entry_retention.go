package proxy

import (
	"context"
	"fmt"
	"strings"
)

// Retain the current route until an explicit entry removal is implemented.
func (s *Service) canvasRetainEntryRoutes(ctx context.Context, plan *canvasPlan, stale *canvasStale) error {
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT inbound_id,coalesce(scheme_id,'') FROM canvas_entry_bindings ORDER BY id")
	if err != nil {
		return err
	}
	type owner struct{ inboundID, schemeID string }
	owners := []owner{}
	for rows.Next() {
		var o owner
		if err := rows.Scan(&o.inboundID, &o.schemeID); err != nil {
			rows.Close()
			return err
		}
		owners = append(owners, o)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	kept := map[string]bool{}
	var retain func(string, string) error
	retain = func(kind, id string) error {
		if id == "" || id == "direct" || id == "block" || kept[kind+":"+id] {
			return nil
		}
		kept[kind+":"+id] = true
		switch kind {
		case "chain":
			c, err := s.Repository.Chain(ctx, id)
			if err != nil {
				return err
			}
			for _, outboundID := range append(c.OutboundIDs, c.FinalOutboundID) {
				if err := retain("outbound", outboundID); err != nil {
					return err
				}
			}
		case "group":
			g, err := s.Repository.Group(ctx, id)
			if err != nil {
				return err
			}
			for _, member := range g.Members {
				if err := retain("outbound", member.OutboundID); err != nil {
					return err
				}
			}
			if err := retain("outbound", g.BackupOutboundID); err != nil {
				return err
			}
		case "outbound":
			o, err := s.Repository.Outbound(ctx, id, false)
			if err != nil {
				return err
			}
			if err := retain("inbound", o.TargetInboundID); err != nil {
				return err
			}
		case "inbound":
			i, err := s.Repository.Inbound(ctx, id, false)
			if err != nil {
				return err
			}
			if strings.HasPrefix(i.DefaultOutbound, "chain:") {
				return retain("chain", strings.TrimPrefix(i.DefaultOutbound, "chain:"))
			}
			if strings.HasPrefix(i.DefaultOutbound, "group:") {
				return retain("group", strings.TrimPrefix(i.DefaultOutbound, "group:"))
			}
			return retain("outbound", i.DefaultOutbound)
		}
		return nil
	}
	for _, o := range owners {
		rerouted := false
		for _, entry := range plan.entries {
			if entry.existing.ID == o.inboundID && (entry.edge != nil || entry.chain != nil || entry.finalOutbound != "") {
				rerouted = true
				break
			}
		}
		if rerouted {
			continue
		}
		if err := retain("inbound", o.inboundID); err != nil {
			return err
		}
		if o.schemeID == plan.schemeID {
			i, err := s.Repository.Inbound(ctx, o.inboundID, false)
			if err != nil {
				return err
			}
			plan.node(i.NodeID)
			stale.warnings = append(stale.warnings, fmt.Sprintf("受管入口 %s 在方案中已没有出站连接，保持原默认出站及依赖对象；撤除须显式操作", i.Name))
		}
	}
	chains := stale.chains[:0]
	for _, c := range stale.chains {
		if kept["chain:"+c.ID] {
			delete(stale.chainID, c.ID)
		} else {
			chains = append(chains, c)
		}
	}
	stale.chains = chains
	outbounds := stale.outbounds[:0]
	for _, o := range stale.outbounds {
		if kept["outbound:"+o.ID] {
			delete(stale.outbound, o.ID)
		} else {
			outbounds = append(outbounds, o)
		}
	}
	stale.outbounds = outbounds
	for key := range kept {
		if strings.HasPrefix(key, "outbound:") {
			delete(stale.outbound, strings.TrimPrefix(key, "outbound:"))
		}
	}
	inbounds := stale.inbounds[:0]
	for _, i := range stale.inbounds {
		if kept["inbound:"+i.ID] {
			delete(stale.inbound, i.ID)
		} else {
			inbounds = append(inbounds, i)
		}
	}
	stale.inbounds = inbounds
	return nil
}
