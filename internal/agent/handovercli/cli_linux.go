//go:build linux

package handovercli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/platform/filelock"
)

func Run(ctx context.Context, o Options) (output string, resultErr error) {
	if os.Geteuid() != 0 {
		return "", core.ErrHandoverPermission
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if o.Operation == "core-handover-protocol" {
		return "1", nil
	}
	if o.ConfigPath == "" {
		return "", errors.New("--config is required")
	}
	c, err := configuration.LoadAgent(o.ConfigPath)
	if err != nil {
		return "", err
	}
	// Serialize root CLI writers without taking the worker's lifecycle lock:
	// arming and revoking must also work while the old worker is running.
	lock, err := filelock.Try("/run/proxy-agent-handover-cli.lock")
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if o.Operation == "finish-core-stop" {
		if _, err := os.Lstat(c.DataDirectory); errors.Is(err, os.ErrNotExist) {
			if err := confirmWorkersExited(o.ConfigPath, c); err != nil {
				return "", fmt.Errorf("worker-stop-unconfirmed: %w", err)
			}
			return "", nil
		} else if err != nil {
			return "", err
		}
		return "", finish(ctx, o.ConfigPath, c)
	}
	s, err := loadLocal(c)
	if err != nil {
		return "", err
	}
	switch o.Operation {
	case "core-handover-target":
		return target(ctx, o.ConfigPath, s)
	case "core-handover-arm":
		return arm(o, s)
	case "core-handover-status":
		return status(o, s)
	case "core-handover-revoke", "core-handover-revoke-all":
		return "", revoke(o, s)
	default:
		return "", errors.New("unknown core handover operation")
	}
}

func arm(o Options, s *localState) (string, error) {
	if o.Scope != "bootstrap" && o.Scope != "worker" || o.Transaction == "" || len(o.Transaction) > 256 || !validDigest(o.TargetSHA256) || o.UpdaterPID <= 0 {
		return "", errors.New("arm requires scope, transaction, lowercase target SHA-256 and updater PID")
	}
	if _, err := core.LoadHandoverRequest(); !errors.Is(err, os.ErrNotExist) {
		return "", errors.Join(core.ErrHandoverConflict, err)
	}
	previous, err := core.LoadHandoverReceipt(s.config.DataDirectory, s.key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if previous != nil && (previous.NodeID != s.nodeID || previous.State != core.HandoverStopped && previous.State != core.HandoverAdopted) {
		return "", core.ErrHandoverConflict
	}
	updater, err := processIdentity(o.UpdaterPID)
	if err != nil {
		return "", err
	}
	source, err := sourceAgent(o.ConfigPath, s.config)
	if err != nil {
		return "", err
	}
	if err := probePidfd(source); err != nil {
		return "", err
	}
	env := core.ObserveOSEnvironment()
	now, err := core.HandoverBoottimeNS()
	if err != nil {
		return "", err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	r := core.HandoverRequest{Version: core.HandoverProtocolVersion, Kind: "core-handover-request", ID: hex.EncodeToString(nonce[:]), NodeID: s.nodeID, BootID: env.BootID, Updater: updater, SourceAgent: source, TargetAgentSHA256: o.TargetSHA256, TransactionID: o.Transaction, Scope: o.Scope, Phase: core.HandoverArmed, CreatedBoottimeNS: now, DeadlineBoottimeNS: now + int64(core.HandoverWindow)}
	if err := authorize(r, s); err != nil {
		return "", err
	}
	if core.ClassifyRecordedProcess(source.PID, source.Token) != core.ProcessAlive {
		return "", core.ErrHandoverConflict
	}
	// Keep evidence across /run loss. Only request.json can authorize anything.
	if err := saveRevoked(s, r); err != nil {
		return "", err
	}
	if err := core.SaveHandoverRequest(r); err != nil {
		return "", err
	}
	return r.ID, nil
}

func authorize(r core.HandoverRequest, s *localState) error {
	return core.AuthorizeHandoverRequest(r, core.HandoverAuthorization{NodeID: s.nodeID, Environment: core.ObserveOSEnvironment(), SourceAgent: r.SourceAgent, TargetAgentSHA256: r.TargetAgentSHA256, TransactionID: r.TransactionID, Scope: r.Scope})
}

func status(o Options, s *localState) (string, error) {
	if !validDigest(o.RequestID) {
		return "", errors.New("valid --request is required")
	}
	r, err := core.LoadHandoverRequest()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	receipt, receiptErr := core.LoadHandoverReceipt(s.config.DataDirectory, s.key)
	if receiptErr != nil && !errors.Is(receiptErr, os.ErrNotExist) {
		return "", receiptErr
	}
	if r == nil {
		if receipt == nil || receipt.RequestID != o.RequestID || receipt.NodeID != s.nodeID {
			return "", core.ErrHandoverConflict
		}
		return string(receipt.State), nil
	}
	if r.ID != o.RequestID || r.NodeID != s.nodeID {
		return "", core.ErrHandoverConflict
	}
	if err := authorize(*r, s); errors.Is(err, core.ErrHandoverExpired) || r.BootID != core.ObserveOSEnvironment().BootID {
		return "expired", nil
	} else if err != nil {
		return "", err
	}
	if receipt == nil || receipt.RequestID != r.ID {
		return "armed", nil
	}
	if err := receipt.MatchRequest(*r); err != nil {
		return "", err
	}
	return string(receipt.State), nil
}

func revoke(o Options, s *localState) error {
	all := o.Operation == "core-handover-revoke-all"
	if all {
		if o.Reason != "uninstall" && o.Reason != "recovery" && o.Reason != "abort" {
			return errors.New("invalid revoke-all reason")
		}
	} else if !validDigest(o.RequestID) || o.Reason != "done" && o.Reason != "rollback" && o.Reason != "abort" {
		return errors.New("invalid revoke request or reason")
	}
	r, err := core.LoadHandoverRequest()
	if errors.Is(err, os.ErrNotExist) {
		if all {
			return nil
		}
		archived, archiveErr := loadRevoked(s, o.RequestID)
		if archiveErr != nil {
			return archiveErr
		}
		if archived.ID != o.RequestID {
			return core.ErrHandoverConflict
		}
		return nil
	}
	if err != nil {
		return err
	}
	if r.NodeID != s.nodeID || !all && r.ID != o.RequestID {
		return core.ErrHandoverConflict
	}
	// Preserve the exact request for a later stopped receipt, after the worker
	// has released lifecycle ownership. This file never grants authorization.
	if err := saveRevoked(s, *r); err != nil {
		return err
	}
	if err := core.RemoveHandoverRequest(*r); err != nil {
		return err
	}
	slog.Info("core handover authorization revoked", "request_id", r.ID, "transaction", r.TransactionID, "scope", r.Scope, "reason", o.Reason)
	return nil
}

func requestMatches(receipt *core.HandoverReceipt, r *core.HandoverRequest, s *localState) error {
	current := core.ObserveOSEnvironment()
	if r.NodeID != s.nodeID || receipt.Environment.HostID != current.HostID || receipt.Environment.BootID == current.BootID && receipt.Environment != current {
		return core.ErrHandoverConflict
	}
	if err := receipt.MatchRequest(*r); err != nil {
		return err
	}
	return nil
}

func runtimeDirectory(root, name string) string { return filepath.Join(root, "runtime", name) }

func conflict(message string) error { return fmt.Errorf("%w: %s", core.ErrHandoverConflict, message) }
