package rules

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// ReconcileImportIdentitiesTx rewrites saved templates in the identity transaction.
// resolve identifies content sources and returns their replacement row IDs.
func ReconcileImportIdentitiesTx(ctx context.Context, tx *sql.Tx, resolve func(sourceID, key string) ([]string, bool)) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM client_policy_profiles ORDER BY id")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var report []string
	for _, id := range ids {
		current, err := profileRead(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		next := current
		next.Template = reconcileImportTemplate(current.Template, "profile:"+id, resolve, &report)
		if err := Validate(ctx, next.Template, nil); err != nil {
			return nil, err
		}
		if _, _, err := updateProfile(ctx, tx, current, next, "import_identity_reconcile"); err != nil {
			return nil, err
		}
	}
	current, err := read(ctx, tx)
	if err != nil {
		return nil, err
	}
	if current.Revision != 0 {
		next := reconcileImportTemplate(current.Template, "system_settings:client_rules", resolve, &report)
		if err := Validate(ctx, next, nil); err != nil {
			return nil, err
		}
		if _, _, err := (&Store{}).saveTx(ctx, tx, next, current.Revision, "import_identity_reconcile", false); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func reconcileImportTemplate(template Template, object string, resolve func(string, string) ([]string, bool), report *[]string) Template {
	template.Groups = slices.Clone(template.Groups)
	for i := range template.Groups {
		group := &template.Groups[i]
		location := fmt.Sprintf("%s groups[%d]", object, i)
		rewrite := func(members []string, field string) []string {
			if len(members) == 0 {
				return members
			}
			result := []string{}
			seen := map[string]bool{}
			appendMember := func(member string) {
				if !seen[member] {
					result = append(result, member)
					seen[member] = true
				}
			}
			for _, member := range members {
				var source, key string
				switch {
				case strings.HasPrefix(member, MemberSource):
					source = strings.TrimPrefix(member, MemberSource)
				case strings.HasPrefix(member, MemberEndpoint+"import:"):
					source, key, _ = strings.Cut(strings.TrimPrefix(member, MemberEndpoint+"import:"), ":")
				default:
					appendMember(member)
					continue
				}
				ids, content := resolve(source, key)
				if !content {
					appendMember(member)
					continue
				}
				if len(ids) == 0 {
					if key != "" {
						appendMember(member)
						*report = append(*report, location+"."+field+": unresolved "+member+" retained")
					} else {
						*report = append(*report, location+"."+field+": empty source "+member+" expanded to no endpoints")
					}
					continue
				}
				for _, id := range ids {
					appendMember(MemberEndpoint + "import:" + source + ":id:" + id)
				}
			}
			return result
		}
		original := group.Members
		group.Members = rewrite(original, "members")
		if len(group.Members) == 0 && len(original) != 0 {
			// The schema requires a member. Retain the original empty source
			// and report that it could not be frozen, as the migration permits.
			group.Members = []string{original[0]}
			*report = append(*report, location+".members: cannot convert empty group; retained "+original[0])
		}
		group.ExcludeEndpoints = rewrite(group.ExcludeEndpoints, "excludeEndpoints")
	}
	return template
}
