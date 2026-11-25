package control

import (
	"log/slog"
	"strings"

	"proxycontrol/internal/agent/reconcile"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// sourceCheckFailedCode is the heartbeat code of a source check that could not
// run; the Agent log carries the error.
const sourceCheckFailedCode = "source_check_failed"

// sourceWatch turns the reconciler's source check into the heartbeat field and
// logs only changes, so a lasting outage does not repeat every heartbeat.
type sourceWatch struct {
	logger *slog.Logger
	last   string
}

func (w *sourceWatch) status(manager *reconcile.Reconciler) *pb.SourceAddressStatus {
	revision, issues, err := manager.MissingSources()
	status := &pb.SourceAddressStatus{Revision: revision}
	if err != nil {
		status.Error = sourceCheckFailedCode
		w.report("error:"+err.Error(), func() {
			w.logger.Error("outbound source check failed", "revision", revision, "error", err)
		})
		return status
	}
	described := make([]string, 0, len(issues))
	for _, issue := range issues {
		status.Missing = append(status.Missing, &pb.SourceAddressIssue{Kind: issue.Kind, OwnerId: issue.OwnerID, Address: issue.Address, Interface: issue.Interface})
		described = append(described, issue.Kind+":"+issue.OwnerID+":"+issue.Address+"%"+issue.Interface)
	}
	signature := strings.Join(described, ",")
	w.report(signature, func() {
		if signature == "" {
			w.logger.Info("configured outbound sources are present again", "revision", revision)
			return
		}
		w.logger.Warn("configured outbound source missing on this host; affected traffic fails until it returns or a new revision is published", "revision", revision, "missing", signature)
	})
	return status
}

func (w *sourceWatch) report(signature string, log func()) {
	if signature == w.last {
		return
	}
	w.last = signature
	log()
}
