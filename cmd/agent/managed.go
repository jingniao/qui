package main

import (
	"context"
	"log/slog"
	"os"

	"proxycontrol/internal/agent/agentprocess"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/platform/config"
)

func runManaged(ctx context.Context, c config.Agent, logger *slog.Logger) error {
	request, channel, err := agentprocess.ReadRequest(os.Stdin)
	if err != nil || request.Version != version {
		return agentprocess.ErrHandshake
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		// Any additional input, malformed data or EOF stops this worker. No
		// command interpreter is exposed through the local lifetime pipe.
		var unexpected [1]byte
		_, _ = channel.Read(unexpected[:])
		cancel()
	}()
	return control.RunWithStartup(workerCtx, c, logger, control.Startup{NodeID: request.NodeID, Revision: request.Revision, AllowDegraded: request.AllowDegraded, AgentUpdates: request.AgentUpdates, AgentRecovery: request.AgentRecovery, ControlOnly: request.ControlOnly, ConnectionFormats: request.ConnectionFormats, SuccessReceiptFormats: request.SuccessReceiptFormats, Ready: func(node string, revision int64, healthy bool) error {
		if err := workerCtx.Err(); err != nil {
			return err
		}
		return agentprocess.ConfirmHealth(request, node, revision, version, healthy, os.Stdout)
	}}, version)
}
