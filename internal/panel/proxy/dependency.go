package proxy

import "errors"

// ErrRelayDependency crosses the publisher interface without an import cycle.
var ErrRelayDependency = errors.New("relay dependency not ready")

// ErrRemovalApply marks a removal phase an Agent refused or rolled back; the
// node kept its previous configuration and the message names it.
var ErrRemovalApply = errors.New("canvas removal not applied")
