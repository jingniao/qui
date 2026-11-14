package proxy

import (
	"context"
	"database/sql"
	"errors"
)

// CanvasEntryBindingRecord preserves ownership and the original route independently of drafts.
type CanvasEntryBindingRecord struct {
	ID                      string
	SchemeID                *string
	InstanceID              string
	InboundID               string
	Mode                    string
	OriginalDefaultOutbound string
	AppliedDefaultOutbound  string
	State                   string
	RequestID               *string
	UpdatedAt               string
	CreationConfigJSON      *string
	CreationGroupsJSON      *string
}

const canvasEntryBindingColumns = "id,scheme_id,instance_id,inbound_id,mode,original_default_outbound,applied_default_outbound,state,request_id,updated_at,creation_config_json,creation_groups_json"

func scanCanvasEntryBinding(row interface{ Scan(...any) error }) (CanvasEntryBindingRecord, error) {
	var b CanvasEntryBindingRecord
	err := row.Scan(&b.ID, &b.SchemeID, &b.InstanceID, &b.InboundID, &b.Mode, &b.OriginalDefaultOutbound, &b.AppliedDefaultOutbound, &b.State, &b.RequestID, &b.UpdatedAt, &b.CreationConfigJSON, &b.CreationGroupsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return CanvasEntryBindingRecord{}, ErrNotFound
	}
	return b, err
}

func (r *Repository) CanvasEntryBinding(ctx context.Context, schemeID, instanceID string) (CanvasEntryBindingRecord, error) {
	return scanCanvasEntryBinding(r.Database.QueryRowContext(ctx, "SELECT "+canvasEntryBindingColumns+" FROM canvas_entry_bindings WHERE scheme_id=? AND instance_id=?", schemeID, instanceID))
}

func (r *Repository) CanvasEntryBindingByInbound(ctx context.Context, inboundID string) (CanvasEntryBindingRecord, error) {
	return scanCanvasEntryBinding(r.Database.QueryRowContext(ctx, "SELECT "+canvasEntryBindingColumns+" FROM canvas_entry_bindings WHERE inbound_id=?", inboundID))
}

func (r *Repository) InsertCanvasEntryBinding(ctx context.Context, b CanvasEntryBindingRecord) error {
	_, err := r.Database.ExecContext(ctx, "INSERT INTO canvas_entry_bindings("+canvasEntryBindingColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", b.ID, b.SchemeID, b.InstanceID, b.InboundID, b.Mode, b.OriginalDefaultOutbound, b.AppliedDefaultOutbound, b.State, b.RequestID, timestamp(), b.CreationConfigJSON, b.CreationGroupsJSON)
	return err
}

func (r *Repository) UpdateCanvasEntryBinding(ctx context.Context, bindingID, state, appliedDefaultOutbound string) error {
	result, err := r.Database.ExecContext(ctx, "UPDATE canvas_entry_bindings SET state=?,applied_default_outbound=?,updated_at=? WHERE id=?", state, appliedDefaultOutbound, timestamp(), bindingID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Capture the takeover route and switch it in the same transaction.
func (r *Repository) applyCanvasEntryBinding(ctx context.Context, b CanvasEntryBindingRecord, target string, create bool) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if create {
		if b.Mode == "reuse" {
			if err := tx.QueryRowContext(ctx, "SELECT default_outbound FROM inbounds WHERE id=?", b.InboundID).Scan(&b.OriginalDefaultOutbound); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO canvas_entry_bindings("+canvasEntryBindingColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", b.ID, b.SchemeID, b.InstanceID, b.InboundID, b.Mode, b.OriginalDefaultOutbound, target, "applied", b.RequestID, timestamp(), b.CreationConfigJSON, b.CreationGroupsJSON)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE canvas_entry_bindings SET applied_default_outbound=?,state='applied',updated_at=? WHERE id=?", target, timestamp(), b.ID)
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE inbounds SET default_outbound=? WHERE id=?", target, b.InboundID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) undoCanvasEntryBinding(ctx context.Context, bindingID, inboundID, route string, previous CanvasEntryBindingRecord, newBinding, createdInbound bool) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if newBinding {
		_, err = tx.ExecContext(ctx, "DELETE FROM canvas_entry_bindings WHERE id=?", bindingID)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE canvas_entry_bindings SET applied_default_outbound=?,state=?,updated_at=? WHERE id=?", previous.AppliedDefaultOutbound, previous.State, previous.UpdatedAt, bindingID)
	}
	if err != nil {
		return err
	}
	if !createdInbound {
		if _, err := tx.ExecContext(ctx, "UPDATE inbounds SET default_outbound=? WHERE id=?", route, inboundID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
