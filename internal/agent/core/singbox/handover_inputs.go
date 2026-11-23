package singbox

import (
	"encoding/json"
	"net"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/scheduler"
	"strconv"
)

// RestoreHandoverInputs must precede Render. The coordinator first registers all
// cores' PrivatePorts together in PortAllocator; this method never binds them.
func (a *Adapter) RestoreHandoverInputs(material core.RuntimeMaterial) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process != nil || a.detached {
		return core.ErrHandoverConflict
	}
	if err := material.Validate(); err != nil {
		return err
	}
	if material.State != core.RuntimeActive || material.Run.NodeID != a.NodeID || material.Run.Core != a.Name() {
		return core.ErrHandoverInvalid
	}
	return a.restorePrivateInputsLocked(material)
}
func (a *Adapter) restorePrivateInputsLocked(material core.RuntimeMaterial) error {
	var doc struct {
		Experimental *experimentalOptions `json:"experimental"`
	}
	if err := json.Unmarshal(material.Rendered.Data, &doc); err != nil {
		return err
	}
	portFor := func(endpoint, purpose string) (int, error) {
		if endpoint == "" {
			return 0, nil
		}
		host, text, err := net.SplitHostPort(endpoint)
		if err != nil {
			return 0, err
		}
		port, err := strconv.Atoi(text)
		if err != nil {
			return 0, err
		}
		if host != "127.0.0.1" || port < 1 || port > 65535 {
			return 0, core.ErrHandoverInvalid
		}
		for _, saved := range material.PrivatePorts {
			if saved.Port == port && saved.Purpose == purpose {
				return port, nil
			}
		}
		return 0, core.ErrHandoverInvalid
	}
	statsPort, err := portFor(material.Rendered.StatsEndpoint, "stats")
	if err != nil {
		return err
	}
	userPort, err := portFor(material.Rendered.UserStatsEndpoint, "user-stats")
	if err != nil {
		return err
	}
	if statsPort != 0 && (doc.Experimental == nil || doc.Experimental.ClashAPI == nil || doc.Experimental.ClashAPI.ExternalController != material.Rendered.StatsEndpoint || doc.Experimental.ClashAPI.Secret != material.ClashSecret || material.ClashSecret == "") {
		return core.ErrHandoverInvalid
	}
	if userPort != 0 && (doc.Experimental == nil || doc.Experimental.V2rayAPI == nil || doc.Experimental.V2rayAPI.Listen != material.Rendered.UserStatsEndpoint) {
		return core.ErrHandoverInvalid
	}
	a.statsPort, a.userStatsPort, a.statsSecret = statsPort, userPort, material.ClashSecret
	a.v2rayAPI, a.nativeClashAPI = userPort != 0, statsPort != 0
	a.probePorts = make(map[string]int, len(material.ProbePorts))
	for key, port := range material.ProbePorts {
		a.probePorts[key] = port
	}
	a.current = material.Rendered
	a.outboundHealth = make(map[string]scheduler.Health, len(material.OutboundHealth))
	for key, health := range material.OutboundHealth {
		a.outboundHealth[key] = health
	}
	return nil
}
