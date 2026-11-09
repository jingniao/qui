package certificate

import (
	"context"
	"errors"
)

// failure is a run failure carrying exactly one of the fixed codes above.
// Everything that reaches a certificate row, an API response or an audit entry
// is one of these codes; the CA's and Cloudflare's own text stays inside the
// process and is never stored or echoed.
type failure struct{ code string }

func (f *failure) Error() string { return f.code }

func fail(code string) error { return &failure{code: code} }

// failureCode maps any error to the code a row records. A cancelled job (shutdown,
// in-process restore) is an interruption, not a failure of the CA or Cloudflare.
func failureCode(err error) string {
	var f *failure
	if errors.As(err, &f) {
		return f.code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CodeInterrupted
	}
	return CodeInternal
}
