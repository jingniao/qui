package proxy

import "errors"

// ErrRelayDependency crosses the publisher interface without an import cycle.
var ErrRelayDependency = errors.New("relay dependency not ready")
