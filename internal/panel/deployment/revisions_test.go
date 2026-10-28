package deployment

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"strings"
	"testing"
	"time"
)

// revisionHistory seeds the history the way this package stores it: one revision
// row per publish plus the deployment target that binds it to its node.
type revisionHistory struct {
	repository *Repository
	ctx        context.Context
	nodeA      string
	nodeB      string
}

func newRevisionHistory(t *testing.T) *revisionHistory {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	nodes := &node.Repository{Database: database}
	first, _, _, err := nodes.CreateEnrollment(ctx, "Node A", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := nodes.CreateEnrollment(ctx, "Node B", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &revisionHistory{repository: &Repository{Database: database}, ctx: ctx, nodeA: first.ID, nodeB: second.ID}
}

func (h *revisionHistory) publish(t *testing.T, summary, nodeID string) int64 {
	t.Helper()
	result, err := h.repository.Database.ExecContext(h.ctx, "INSERT INTO config_revisions(author,summary,status,created_at,encrypted_state) VALUES('owner',?,'success','x',x'00')", summary)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repository.Database.ExecContext(h.ctx, "INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'success','x')", id, nodeID); err != nil {
		t.Fatal(err)
	}
	return id
}

type revisionPage struct {
	Items []Revision `json:"items"`
	Total int        `json:"total"`
}

func revisionList(t *testing.T, handler *Handler, query string) revisionPage {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.list(recorder, httptest.NewRequest("GET", "/api/v1/revisions"+query, nil))
	if recorder.Code != 200 {
		t.Fatalf("list%s: %d %s", query, recorder.Code, recorder.Body.String())
	}
	var page revisionPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

// TestRevisionListFiltersByNode: the configuration history can be narrowed to one
// node. The page and the total must both follow the filter, otherwise the pager
// promises a second page that is empty.
func TestRevisionListFiltersByNode(t *testing.T) {
	history := newRevisionHistory(t)
	handler := &Handler{Repository: history.repository}
	firstA := history.publish(t, "A first", history.nodeA)
	onlyB := history.publish(t, "B only", history.nodeB)
	secondA := history.publish(t, "A second", history.nodeA)

	page := revisionList(t, handler, "?nodeId="+history.nodeA)
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("node history reports total %d with %d items", page.Total, len(page.Items))
	}
	if page.Items[0].ID != secondA || page.Items[1].ID != firstA {
		t.Fatalf("node history is not newest first: %+v", page.Items)
	}
	for _, item := range page.Items {
		if item.NodeID != history.nodeA {
			t.Fatalf("node history carries another node's revision: %+v", item)
		}
	}

	// Without a filter the same list holds every revision of every node.
	page = revisionList(t, handler, "")
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("unfiltered history reports total %d with %d items", page.Total, len(page.Items))
	}
	if page.Items[0].ID != secondA || page.Items[1].ID != onlyB || page.Items[2].ID != firstA {
		t.Fatalf("unfiltered history is not newest first: %+v", page.Items)
	}
}

func TestRevisionListRejectsOversizedNodeId(t *testing.T) {
	// The bound is enforced before the repository is used, so no database is
	// needed here.
	recorder := httptest.NewRecorder()
	(&Handler{}).list(recorder, httptest.NewRequest("GET", "/api/v1/revisions?nodeId="+strings.Repeat("a", 129), nil))
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "Invalid nodeId") {
		t.Fatalf("oversized nodeId: %d %s", recorder.Code, recorder.Body.String())
	}
}
