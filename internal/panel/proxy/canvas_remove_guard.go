package proxy

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
)

// Verify the captured ownership graph inside the deletion transaction so a
// concurrent save cannot introduce a reference between calculation and deletion.
type canvasRemovalRow struct {
	query  string
	id     string
	values [][]any
}

type canvasRemovalReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readCanvasRemovalRows(ctx context.Context, db canvasRemovalReader, query, id string) ([][]any, error) {
	rows, err := db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := [][]any{}
	for rows.Next() {
		row := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range row {
			pointers[i] = &row[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	return values, rows.Err()
}

func (s *Service) canvasRemovalRows(ctx context.Context, plan CanvasRemovePlan) ([]canvasRemovalRow, error) {
	guards := []canvasRemovalRow{}
	add := func(query, id string) error {
		values, err := readCanvasRemovalRows(ctx, s.Repository.Database, query, id)
		if err != nil {
			return err
		}
		guards = append(guards, canvasRemovalRow{query: query, id: id, values: values})
		return nil
	}
	for _, entry := range plan.Entries {
		if err := add("SELECT * FROM canvas_entry_bindings WHERE inbound_id=?", entry.InboundID); err != nil {
			return nil, err
		}
		if err := add("SELECT * FROM inbounds WHERE id=?", entry.InboundID); err != nil {
			return nil, err
		}
		if err := add(canvasReferrersQuery, entry.InboundID); err != nil {
			return nil, err
		}
		for _, query := range []string{
			"SELECT id,target_inbound_id FROM tunnel_rules WHERE target_inbound_id=? ORDER BY id",
			"SELECT s.id,s.layout_json FROM canvas_schemes s WHERE EXISTS(SELECT 1 FROM json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.ref')='inbound:'||?1) ORDER BY s.id",
		} {
			if err := add(query, entry.InboundID); err != nil {
				return nil, err
			}
		}
	}
	for _, object := range plan.Objects {
		if object.Keep {
			continue
		}
		if err := add("SELECT * FROM "+canvasRemoveTable(object.Kind)+" WHERE id=?", object.ID); err != nil {
			return nil, err
		}
		if err := add(canvasReferrersQuery, object.ID); err != nil {
			return nil, err
		}
		for _, query := range []string{
			"SELECT id,action_json FROM policies WHERE json_extract(action_json,'$.target_id')=?1 OR json_extract(action_json,'$.backup_id')=?1 ORDER BY id",
			"SELECT id,target_inbound_id FROM tunnel_rules WHERE target_inbound_id=? ORDER BY id",
			"SELECT s.id,s.layout_json FROM canvas_schemes s WHERE EXISTS(SELECT 1 FROM json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.ref') IN ('inbound:'||?1,'outbound:'||?1)) ORDER BY s.id",
		} {
			if err := add(query, object.ID); err != nil {
				return nil, err
			}
		}
		if object.Kind == "chain" {
			if err := add("SELECT * FROM proxy_chain_hops WHERE chain_id=? ORDER BY position", object.ID); err != nil {
				return nil, err
			}
		}
	}
	for _, tunnelID := range plan.unsharedTunnelIDs {
		for _, query := range []string{
			canvasTunnelSchemesQuery,
			"SELECT * FROM canvas_tunnel_publications WHERE tunnel_id=? ORDER BY scheme_id",
			"SELECT * FROM canvas_tunnel_objects WHERE tunnel_id=? ORDER BY object_kind,object_id",
			"SELECT * FROM canvas_tunnels WHERE id=?",
		} {
			if err := add(query, tunnelID); err != nil {
				return nil, err
			}
		}
	}
	return guards, nil
}

func checkCanvasRemovalRows(ctx context.Context, tx *sql.Tx, guards []canvasRemovalRow) error {
	for _, guard := range guards {
		values, err := readCanvasRemovalRows(ctx, tx, guard.query, guard.id)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(values, guard.values) {
			return fmt.Errorf("撤除对象 %s 在删除前已修改，请刷新后重试", guard.id)
		}
	}
	return nil
}
