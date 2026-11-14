package rules

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

type ImportedEndpointUsage struct {
	ProfileID   string   `json:"profileId"`
	ProfileName string   `json:"profileName"`
	Groups      []string `json:"groups"`
	Active      bool     `json:"active"`
}

// ImportedEndpointRemovalError preserves the normal save validation error and
// identifies the policy groups that deletion would leave without members.
type ImportedEndpointRemovalError struct {
	ProfileID   string   `json:"profileId"`
	ProfileName string   `json:"profileName"`
	Groups      []string `json:"groups"`
	Err         error    `json:"-"`
}

func (e *ImportedEndpointRemovalError) Error() string {
	if len(e.Groups) != 0 {
		return fmt.Sprintf("客户端策略“%s”的策略组“%s”会被删空，请先修改策略组", e.ProfileName, strings.Join(e.Groups, "、"))
	}
	return fmt.Sprintf("客户端策略“%s”删除节点引用后校验失败：%v", e.ProfileName, e.Err)
}

func (e *ImportedEndpointRemovalError) Unwrap() error { return e.Err }

func importedEndpointProfiles(ctx context.Context, tx *sql.Tx) ([]Profile, string, Stored, error) {
	active, err := activeID(ctx, tx)
	if err != nil {
		return nil, "", Stored{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM client_policy_profiles ORDER BY id")
	if err != nil {
		return nil, "", Stored{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, "", Stored{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", Stored{}, err
	}
	profiles := make([]Profile, 0, len(ids))
	for _, id := range ids {
		p, err := profileRead(ctx, tx, id)
		if err != nil {
			return nil, "", Stored{}, err
		}
		profiles = append(profiles, p)
	}
	snapshot, err := read(ctx, tx)
	return profiles, active, snapshot, err
}

func importedEndpointGroups(template Template, member string) []string {
	groups := []string{}
	for _, group := range template.Groups {
		if slices.Contains(group.Members, member) || slices.Contains(group.ExcludeEndpoints, member) {
			groups = append(groups, group.Name)
		}
	}
	return groups
}

// ImportedEndpointUsageTx includes exact references in all saved profiles and
// the active snapshot. Dynamic selectors do not persist an endpoint reference.
func ImportedEndpointUsageTx(ctx context.Context, tx *sql.Tx, sourceID, proxyID string) ([]ImportedEndpointUsage, error) {
	profiles, active, snapshot, err := importedEndpointProfiles(ctx, tx)
	if err != nil {
		return nil, err
	}
	member := MemberEndpoint + "import:" + sourceID + ":id:" + proxyID
	refs := []ImportedEndpointUsage{}
	for _, p := range profiles {
		groups := importedEndpointGroups(p.Template, member)
		if p.ID == active {
			for _, name := range importedEndpointGroups(snapshot.Template, member) {
				if !slices.Contains(groups, name) {
					groups = append(groups, name)
				}
			}
		}
		if len(groups) != 0 {
			refs = append(refs, ImportedEndpointUsage{ProfileID: p.ID, ProfileName: p.Name, Groups: groups, Active: p.ID == active})
		}
	}
	return refs, nil
}

func removeImportedEndpoint(ctx context.Context, template Template, member, profileID, profileName string) (Template, bool, error) {
	template.Groups = slices.Clone(template.Groups)
	changed := false
	empty := []string{}
	remove := func(values []string) []string {
		if !slices.Contains(values, member) {
			return values
		}
		changed = true
		return slices.DeleteFunc(slices.Clone(values), func(value string) bool { return value == member })
	}
	for i := range template.Groups {
		group := &template.Groups[i]
		previous := len(group.Members)
		group.Members = remove(group.Members)
		group.ExcludeEndpoints = remove(group.ExcludeEndpoints)
		if previous != 0 && len(group.Members) == 0 {
			empty = append(empty, group.Name)
		}
	}
	if changed {
		if err := Validate(ctx, template, nil); err != nil {
			return Template{}, false, &ImportedEndpointRemovalError{ProfileID: profileID, ProfileName: profileName, Groups: empty, Err: err}
		}
	}
	return template, changed, nil
}

// RemoveImportedEndpointTx shares the caller's deletion transaction. It uses
// profile and snapshot save paths so concurrent editors observe new revisions.
func RemoveImportedEndpointTx(ctx context.Context, tx *sql.Tx, sourceID, proxyID string) error {
	profiles, active, snapshot, err := importedEndpointProfiles(ctx, tx)
	if err != nil {
		return err
	}
	member := MemberEndpoint + "import:" + sourceID + ":id:" + proxyID
	activeName := ""
	for _, current := range profiles {
		if current.ID == active {
			activeName = current.Name
		}
		next := current
		var changed bool
		next.Template, changed, err = removeImportedEndpoint(ctx, current.Template, member, current.ID, current.Name)
		if err != nil {
			return err
		}
		if changed {
			if _, _, err := updateProfile(ctx, tx, current, next, "import_node_delete"); err != nil {
				return err
			}
		}
	}
	next, changed, err := removeImportedEndpoint(ctx, snapshot.Template, member, active, activeName)
	if err != nil {
		return err
	}
	if changed {
		_, _, err = (&Store{}).saveTx(ctx, tx, next, snapshot.Revision, "import_node_delete", false)
	}
	return err
}
