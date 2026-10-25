package control

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// trafficRejected reports whether the panel ended the control stream with the
// deterministic telemetry rejection it sends for a batch it cannot accept.
// Transport failures, EOF and cancellation stay retryable and must never count
// against a queued batch.
func trafficRejected(err error) bool {
	switch status.Code(err) {
	case codes.FailedPrecondition, codes.InvalidArgument:
		return true
	}
	return false
}
