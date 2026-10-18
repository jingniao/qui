package user

import (
	"context"
	"strings"
	"time"
)

type Access struct {
	User        User
	Credentials []Credential
}

func (r *Repository) AccessForGroups(ctx context.Context, groups []string) ([]Access, error) {
	if len(groups) == 0 {
		return []Access{}, nil
	}
	if len(groups) > 100 {
		return nil, ErrCredential
	}
	placeholders := make([]string, len(groups))
	arguments := make([]any, len(groups))
	for index, group := range groups {
		placeholders[index] = "?"
		arguments[index] = group
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT DISTINCT user_id FROM user_node_group_grants WHERE group_id IN ("+strings.Join(placeholders, ",")+") ORDER BY user_id", arguments...)
	if err != nil {
		return nil, err
	}
	userIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		userIDs = append(userIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	access := []Access{}
	for _, userID := range userIDs {
		u, err := r.Get(ctx, userID)
		if err != nil {
			return nil, err
		}
		if !u.Active(time.Now()) {
			continue
		}
		devices, _, err := r.Devices(ctx, userID, 1, 100)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, protocol := range u.AllowedProtocols {
			allowed[protocol] = true
		}
		entry := Access{User: u, Credentials: []Credential{}}
		for _, device := range devices {
			if device.Status != "active" {
				continue
			}
			credentials, err := r.Credentials(ctx, device.ID, true)
			if err != nil {
				return nil, err
			}
			for _, credential := range credentials {
				if credential.Status == "active" && allowed[credential.Protocol] {
					entry.Credentials = append(entry.Credentials, credential)
				}
			}
		}
		access = append(access, entry)
	}
	return access, nil
}
