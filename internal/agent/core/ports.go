package core

import (
	"errors"
	"net"
	"sort"
	"sync"
)

// PortAllocator reserves private ports for the lifetime of the Agent. The OS
// still arbitrates external races at apply time; core rollback handles them.
type PortAllocator struct {
	mu      sync.Mutex
	private map[int]bool
	public  map[int]bool
}

func (p *PortAllocator) ReserveAdditionalInbounds(ports []int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.public == nil {
		p.public = map[int]bool{}
	}
	for _, port := range ports {
		if p.private[port] {
			return errors.New("inbound conflicts with reserved runtime port")
		}
	}
	for _, port := range ports {
		p.public[port] = true
	}
	return nil
}

func (p *PortAllocator) ReserveInbounds(ports []int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	next := map[int]bool{}
	for _, port := range ports {
		if p.private[port] {
			return errors.New("inbound conflicts with reserved runtime port")
		}
		next[port] = true
	}
	p.public = next
	return nil
}

func (p *PortAllocator) Allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.private == nil {
		p.private = map[int]bool{}
	}
	for i := 0; i < 128; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if !p.public[port] && !p.private[port] {
			p.private[port] = true
			return port, nil
		}
	}
	return 0, errors.New("no free private core port")
}

func (p *PortAllocator) Ports() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	ports := []int{}
	for port := range p.private {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}
