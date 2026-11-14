package proxy

import (
	"context"
	"database/sql"
	"fmt"
)

type ImportCanvasUsage struct {
	SchemeID   string `json:"schemeId"`
	SchemeName string `json:"schemeName"`
}

// ImportedProxyCanvasesTx includes both drafts and the last published client
// snapshot; editing a draft alone must not permit deleting a published hop.
func ImportedProxyCanvasesTx(ctx context.Context, tx *sql.Tx, proxyID string) ([]ImportCanvasUsage, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,name,layout_json,client_published_layout FROM canvas_schemes ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	canvases := []ImportCanvasUsage{}
	for rows.Next() {
		var usage ImportCanvasUsage
		var draft, published string
		if err := rows.Scan(&usage.SchemeID, &usage.SchemeName, &draft, &published); err != nil {
			return nil, err
		}
		found := false
		for _, snapshot := range []string{draft, published} {
			if snapshot == "" {
				continue
			}
			layout, err := decodeCanvasLayout([]byte(snapshot))
			if err != nil {
				return nil, fmt.Errorf("canvas %s import usage: %w", usage.SchemeID, err)
			}
			for _, instance := range layout.Instances {
				if instance.Ref == "import:"+proxyID {
					found = true
				}
			}
		}
		if found {
			canvases = append(canvases, usage)
		}
	}
	return canvases, rows.Err()
}
