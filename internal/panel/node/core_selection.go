package node

import (
	"context"
	"sort"
)

// Capabilities remain the enabled inventory. Additions are selectable only after
// both a successful task and a report containing that component; removal keeps
// the component visible until the Agent confirms its new set.
func CoreSelectionIsBlocked(n Node, core string) bool {
	if core == "" {
		core = n.Capabilities.GetCoreName()
	}
	for _, blocked := range n.CoreSelectionBlocked {
		if blocked == core {
			return true
		}
	}
	return false
}

// The newest add task per component decides whether an addition is still open;
// an active removal blocks the component in the other direction.
const coreSelectionQuery = `SELECT t.node_id,t.component FROM core_update_tasks t
 WHERE t.adding=1 AND t.status<>'success' AND NOT EXISTS(SELECT 1 FROM core_update_tasks newer WHERE newer.node_id=t.node_id AND newer.component=t.component AND (newer.created_at>t.created_at OR newer.created_at=t.created_at AND newer.rowid>t.rowid))
 UNION SELECT node_id,component FROM core_remove_tasks WHERE status IN ('queued','running')`

func (r *Repository) decorateCoreSelection(ctx context.Context, nodes []Node) error {
	byID := map[string]*Node{}
	for i := range nodes {
		nodes[i].CoreSelectionBlocked = []string{}
		byID[nodes[i].ID] = &nodes[i]
	}
	rows, err := r.Database.QueryContext(ctx, coreSelectionQuery)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, core string
		if err := rows.Scan(&id, &core); err != nil {
			return err
		}
		if n := byID[id]; n != nil {
			n.CoreSelectionBlocked = append(n.CoreSelectionBlocked, core)
		}
	}
	for i := range nodes {
		sort.Strings(nodes[i].CoreSelectionBlocked)
	}
	return rows.Err()
}
