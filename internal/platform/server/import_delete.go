package server

import (
	"context"
	"database/sql"
	"errors"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/rules"
)

type importDeletionReferences struct{}

func (importDeletionReferences) Preview(ctx context.Context, tx *sql.Tx, src, key string) ([]importer.DeletionGroup, []importer.DeletionPolicy, []importer.DeletionCanvas, error) {
	groups := []importer.DeletionGroup{}
	policies := []importer.DeletionPolicy{}
	canvases := []importer.DeletionCanvas{}
	groupRefs, err := node.ImportedProxyGroupsTx(ctx, tx, src, key)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, g := range groupRefs {
		groups = append(groups, importer.DeletionGroup{ID: g.ID, Name: g.Name})
	}
	policyRefs, err := rules.ImportedEndpointUsageTx(ctx, tx, src, key)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, p := range policyRefs {
		policies = append(policies, importer.DeletionPolicy{ProfileID: p.ProfileID, ProfileName: p.ProfileName, Groups: p.Groups, Active: p.Active})
	}
	canvasRefs, err := proxy.ImportedProxyCanvasesTx(ctx, tx, key)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, c := range canvasRefs {
		canvases = append(canvases, importer.DeletionCanvas{SchemeID: c.SchemeID, SchemeName: c.SchemeName})
	}
	return groups, policies, canvases, nil
}
func (importDeletionReferences) Remove(ctx context.Context, tx *sql.Tx, src, key string) error {
	if err := node.RemoveImportedProxyGroupsTx(ctx, tx, src, key); err != nil {
		return err
	}
	if err := rules.RemoveImportedEndpointTx(ctx, tx, src, key); err != nil {
		var invalid *rules.ImportedEndpointRemovalError
		if errors.As(err, &invalid) {
			return &importer.DeletionConflict{Code: "import_referenced", Message: invalid.Error(), Details: map[string]any{"clientPolicies": []*rules.ImportedEndpointRemovalError{invalid}}}
		}
		return err
	}
	return nil
}
