package access

// batchRejection is a refused access or traffic batch with a fixed reason code
// and, when one record caused it, that record's revision. The panel logs the
// code instead of free text, so the log never carries batch contents.
type batchRejection struct {
	reason   string
	revision int64
	cause    error
}

func (e *batchRejection) Error() string { return "batch rejected: " + e.reason }
func (e *batchRejection) Unwrap() error { return e.cause }

// RejectionReason reports the fixed reason code and the revision involved (0
// when unknown). trafficProcessingError has the same method; errors without
// it, such as malformed batches, are logged as "invalid".
func (e *batchRejection) RejectionReason() (string, int64) { return e.reason, e.revision }

func reject(reason string, revision int64) error {
	return &batchRejection{reason: reason, revision: revision}
}

// rejectStore refuses a batch the panel could not read or write, keeping
// storage pressure (busy, locked, full) apart from other failures.
func rejectStore(reason string, err error) error {
	if trafficStorageFailure(err) {
		reason = "storage_failure"
	}
	return &batchRejection{reason: reason, cause: err}
}
