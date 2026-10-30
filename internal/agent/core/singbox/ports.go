package singbox

import "proxycontrol/internal/agent/core"

func (a *Adapter) SetPortAllocator(p *core.PortAllocator) { a.Ports = p }
