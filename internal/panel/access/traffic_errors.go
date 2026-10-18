package access

import "errors"

type trafficProcessingError struct {
	code  string
	cause error
}

func (e *trafficProcessingError) Error() string {
	return "traffic processing remains pending: " + e.code
}
func (e *trafficProcessingError) Unwrap() error { return e.cause }

func trafficStageError(code string, err error) error {
	if trafficStorageFailure(err) {
		code = "storage_failure"
	}
	return &trafficProcessingError{code: code, cause: err}
}

func trafficStorageFailure(err error) bool {
	var databaseError interface{ Code() int }
	if !errors.As(err, &databaseError) {
		return false
	}
	// SQLite primary codes for busy/locked/readonly/I/O/full/cannot-open.
	switch databaseError.Code() & 255 {
	case 5, 6, 8, 10, 13, 14:
		return true
	default:
		return false
	}
}

func (s *Service) trafficAccountError(err error) error {
	code := "accounting_pending"
	if classifier, ok := s.Users.(interface{ TrafficFailureCode(error) string }); ok {
		switch classifier.TrafficFailureCode(err) {
		case "material_invalid":
			code = "material_invalid"
		case "authorization_conflict":
			code = "authorization_conflict"
		case "identity_conflict":
			code = "identity_conflict"
		}
	}
	return trafficStageError(code, err)
}
