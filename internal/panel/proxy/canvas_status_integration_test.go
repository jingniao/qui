package proxy_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/testutil/dbtest"
)

func TestCanvasStatusReadsRealDeploymentBatch(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	database.SetMaxOpenConns(1)
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Status node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(10,'Owner','status','success','now',X'00')")
	exec("INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at) VALUES('status-batch','','status','Owner','success','now')")
	exec("INSERT INTO deployment_batch_items(batch_id,position,node_id,revision,status) VALUES('status-batch',0,?,10,'success')", n.ID)
	exec("INSERT INTO node_runtime_state(node_id,revision) VALUES(?,10)", n.ID)
	service := &proxy.Service{Repository: &proxy.Repository{Database: database}, Nodes: nodes, Deploy: &deployment.CanvasPublisher{Repository: &deployment.Repository{Database: database}}}
	scheme, err := service.Repository.SaveCanvasScheme(ctx, "", proxy.CanvasSchemeInput{Name: "status", Execution: "server", Layout: proxy.CanvasLayout{Instances: []proxy.CanvasInstance{}, Connections: []proxy.CanvasConnection{}}})
	if err != nil {
		t.Fatal(err)
	}
	exec("UPDATE canvas_schemes SET published_batch_id='status-batch',published_at=updated_at WHERE id=?", scheme.ID)
	exec("PRAGMA query_only=ON")
	status, err := service.CanvasSchemeStatus(ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Batch.ID != "status-batch" || status.Batch.Status != "success" || len(status.Batch.Items) != 1 || status.Batch.Items[0].NodeName != n.Name || len(status.Nodes) != 1 || !status.Nodes[0].Confirmed {
		t.Fatalf("status %+v", status)
	}
}
