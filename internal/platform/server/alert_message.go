package server

import (
	"context"
	"log/slog"

	"proxycontrol/internal/panel/node"
)

// A give-up alert must still reach the Owner if reading its display name fails.
// Keep this data-protection fallback here so it can be removed independently.
func alertNodeName(ctx context.Context, nodes *node.Repository, logger *slog.Logger, id string) string {
	item, err := nodes.Get(ctx, id)
	if err != nil {
		logger.Error("alert node name could not be read", "node", id, "error", err)
		return "节点 " + id + "（名称读取失败）"
	}
	if item.Name == "" {
		return "未命名节点（" + id + "）"
	}
	return "节点「" + item.Name + "」"
}
