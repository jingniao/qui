package proxy

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Only an explicitly owned, paired legacy relay can change protocol. Ordinary
// proxy APIs retain their immutable protocol and target contracts.
func (s *Service) canvasPlanTLSUpgrade(ctx context.Context, plan *canvasPlan, edge *canvasEdge) error {
	if edge.protocol != "trojan" {
		return nil
	}
	objects, err := s.Repository.tunnelObjects(ctx, edge.tunnelID)
	if err != nil {
		return err
	}
	for _, object := range objects {
		if object.Kind != "inbound" || object.NodeID != edge.targetNode {
			continue
		}
		in, err := s.Repository.Inbound(ctx, object.ID, true)
		if err != nil {
			return err
		}
		if !in.InternalRelay || in.Protocol == "trojan" || in.Port != edge.port {
			continue
		}
		for _, candidate := range objects {
			if candidate.Kind != "outbound" || candidate.NodeID != edge.sourceNode {
				continue
			}
			out, err := s.Repository.Outbound(ctx, candidate.ID, true)
			if err != nil {
				return err
			}
			if out.TargetInboundID != in.ID || out.Protocol != in.Protocol {
				continue
			}
			if edge.upgradeOutbound.ID != "" {
				return errors.New("通道有多个旧协议内部监听，无法确定更新对象")
			}
			edge.adopt, edge.upgradeOutbound = in, out
			plan.inbounds[in.Name], plan.outbounds[out.Name] = true, true
		}
	}
	return nil
}

func (s *Service) canvasUpgradeTLS(ctx context.Context, edge *canvasEdge, undo *canvasUndo) error {
	r := s.Repository
	in, out := edge.adopt, edge.upgradeOutbound
	// Preserve exact sealed settings and credentials for compensation; no secrets
	// are written into plans, logs or creation baselines.
	var inSettings, outSettings []byte
	if err := r.Database.QueryRowContext(ctx, "SELECT encrypted_settings FROM inbounds WHERE id=?", in.ID).Scan(&inSettings); err != nil {
		return err
	}
	if err := r.Database.QueryRowContext(ctx, "SELECT encrypted_settings FROM outbounds WHERE id=?", out.ID).Scan(&outSettings); err != nil {
		return err
	}
	empty, err := r.encode(desiredstate.ProtocolSettings{})
	if err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	credential, err := r.createCredential(ctx, tx, "trojan", "", "")
	if err != nil {
		return err
	}
	outboundCredential, err := r.createCredential(ctx, tx, "trojan", "", "")
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE inbounds SET protocol='trojan',name=?,encrypted_settings=?,relay_credential_id=?,tls_source='',certificate_id=NULL WHERE id=?", edge.inboundName, empty, credential.ID, in.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE outbounds SET protocol='trojan',name=?,encrypted_settings=?,credential_id=? WHERE id=?", edge.outboundName, empty, outboundCredential.ID, out.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE relay_bindings SET credential_id=? WHERE outbound_id=?", outboundCredential.ID, out.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	undo.add(func(ctx context.Context) error {
		tx, err := r.Database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, "UPDATE inbounds SET protocol=?,name=?,encrypted_settings=?,relay_credential_id=?,tls_source=?,certificate_id=? WHERE id=?", in.Protocol, in.Name, inSettings, in.RelayCredentialID, in.TLSSource, nullable(in.CertificateID), in.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE outbounds SET protocol=?,name=?,encrypted_settings=?,credential_id=? WHERE id=?", out.Protocol, out.Name, outSettings, out.CredentialID, out.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE relay_bindings SET credential_id=? WHERE outbound_id=?", out.CredentialID, out.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM relay_credentials WHERE id IN (?,?)", credential.ID, outboundCredential.ID); err != nil {
			return err
		}
		return tx.Commit()
	})
	return nil
}
