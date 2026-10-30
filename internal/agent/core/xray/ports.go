package xray

import (
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

func (a *Adapter) SetPortAllocator(p *core.PortAllocator) { a.Ports = p }
func (a *Adapter) privatePortLocked(s desiredstate.State) (int, error) {
	if a.Ports == nil {
		a.Ports = &core.PortAllocator{}
	}
	ports := []int{}
	for _, in := range s.Inbounds {
		if in.Enabled {
			ports = append(ports, in.Port)
		}
	}
	// MultiRuntime reserves the complete node before entering this adapter.
	// A standalone adapter reserves its own listeners on first use only.
	if err := a.Ports.ReserveAdditionalInbounds(ports); err != nil {
		return 0, err
	}
	return a.Ports.Allocate()
}
