package xray

import (
	"proxycontrol/internal/agent/core/scheduling"
	"proxycontrol/pkg/contracts/desiredstate"
)

func schedulingState(original desiredstate.State) (desiredstate.State, map[string]string, error) {
	return scheduling.State(original)
}
