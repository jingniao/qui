package server

import (
	"context"
	"database/sql"
	"fmt"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/panel/user"
	"time"
)

type cycleScheduler struct {
	users    *user.Repository
	nodes    *node.Repository
	settings *system.Repository
	quota    *quotaDeployer
	now      func() time.Time
	publish  func(string, any)
	// refresh pushes a node whose renewal date moved to the runtime display.
	refresh func(context.Context, string) error
}

func (s *cycleScheduler) step(ctx context.Context) error {
	base, err := s.settings.ReadBase(ctx)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(base.Timezone)
	if err != nil {
		return err
	}
	now := s.now()
	ids, err := s.users.ResetDue(ctx, now, location)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		s.quota.evaluate(ctx, ids)
	}
	// Alerts read the date before renewal: auto renewal does not suppress the
	// usual renewal notices, including the one for the passed date.
	nodes, err := s.nodes.RenewalAlerts(ctx, now, location)
	if err != nil {
		return err
	}
	for _, item := range nodes {
		name := item.Name
		if name == "" {
			name = "未命名服务器（节点 " + item.NodeID + "）"
		}
		var message string
		switch {
		case item.DaysLeft > 0:
			message = fmt.Sprintf("服务器「%s」将于 %s 到期，还剩 %d 天。", name, item.RenewDate, item.DaysLeft)
		case item.DaysLeft == 0:
			message = fmt.Sprintf("服务器「%s」今天（%s）到期。", name, item.RenewDate)
		default:
			message = fmt.Sprintf("服务器「%s」已于 %s 到期，已过 %d 天。", name, item.RenewDate, -item.DaysLeft)
		}
		if item.AutoRenew {
			message += "已开启自动续费，日期将自动顺延。"
		}
		s.publish("system.alert", map[string]string{"alert": "node_renewal", "nodeId": item.NodeID, "message": message})
	}
	renewed, err := s.nodes.AutoRenew(ctx, now, location, func(ctx context.Context, tx *sql.Tx, item node.RenewedNode) error {
		return s.settings.RecordTx(ctx, tx, "system", "自动续费 "+item.From+" → "+item.To, item.NodeID, "成功", "")
	})
	if err != nil {
		return err
	}
	for _, item := range renewed {
		if err := s.refresh(ctx, item.NodeID); err != nil {
			return err
		}
	}
	return nil
}

// run shares Serve's cancellation and join boundary with quota and imports.
// Restore therefore waits for all database work before closing the old pool.
func (s *cycleScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.step(work)
		cancel()
		if err != nil && ctx.Err() == nil {
			s.quota.logger.Error("traffic cycle maintenance failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
