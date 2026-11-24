package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestAlertNodeNameAndAccessMessage(t *testing.T) {
	app := testServer(t)
	ctx := context.Background()
	n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "Named", 0)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if got := alertNodeName(ctx, app.nodeRepository, logger, n.ID); got != "节点「Named」" {
		t.Fatal(got)
	}
	if _, err = app.Database.Exec("UPDATE nodes SET name='' WHERE id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	if got := alertNodeName(ctx, app.nodeRepository, logger, n.ID); got != "未命名节点（"+n.ID+"）" {
		t.Fatal(got)
	}
	if got := alertNodeName(ctx, app.nodeRepository, logger, "missing"); got != "节点 missing（名称读取失败）" || !strings.Contains(logs.String(), "error=") {
		t.Fatal(got, logs.String())
	}
	for _, reason := range []string{"disabled", "quota_exceeded", "expired", "deleted", "reactivated"} {
		got := userAccessMessage("alice", reason)
		if !strings.Contains(got, "用户「alice」") || !strings.Contains(got, "下一次自动额度发布") || strings.Contains(got, reason) {
			t.Fatal(got)
		}
	}
	if got := userAccessMessage("alice", "new_code"); !strings.Contains(got, "new_code") {
		t.Fatal(got)
	}
}
