package core

import "errors"

// AddIdle and RemoveIdle are called under the Reconciler's membership lease.
// No revision can be applied concurrently. Adapters retain shared telemetry hooks.
func (m *MultiRuntime) AddIdle(r Runtime) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r == nil || (r.Name() != "xray" && r.Name() != "sing-box") || m.runtimes[r.Name()] != nil {
		return errors.New("invalid new core")
	}
	if a, ok := r.(interface{ SetPortAllocator(*PortAllocator) }); ok {
		a.SetPortAllocator(m.ports)
	}
	r.ConfigureRuntime(m.hooks)
	m.runtimes[r.Name()] = r
	m.names = append(m.names, r.Name())
	m.expected[r.Name()] = false
	return nil
}
func (m *MultiRuntime) RemoveIdle(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runtimes[name]
	if r == nil {
		return errors.New("unknown core")
	}
	if len(m.names) == 1 || m.expected[name] {
		return errors.New("core is required")
	}
	if err := r.Close(); err != nil {
		return err
	}
	delete(m.runtimes, name)
	delete(m.expected, name)
	delete(m.current, name)
	for index, n := range m.names {
		if n == name {
			m.names = append(m.names[:index], m.names[index+1:]...)
			break
		}
	}
	if m.primary == name {
		m.primary = m.names[0]
	}
	return nil
}
func (m *MultiRuntime) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.names...)
}
