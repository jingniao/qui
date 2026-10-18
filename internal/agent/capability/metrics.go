package capability

import (
	"math"
	pb "proxycontrol/pkg/contracts/agentpb"
	"sync"
	"time"
)

type counters struct {
	total, idle uint64
	valid       bool
}
type systemMetrics struct {
	cpu          counters
	memory, disk *float64
}
type Sampler struct {
	mu                  sync.Mutex
	directory           string
	previous            counters
	sampled             time.Time
	CollectorStatus     func() *pb.CollectorStatus
	TrafficSourceStatus func() *pb.TrafficSourceStatus
}

func NewSampler(directory string) *Sampler {
	initial := readSystem(directory)
	return &Sampler{directory: directory, previous: initial.cpu, sampled: time.Now()}
}
func percent(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	value = max(0, min(100, value))
	return &value
}
func (s *Sampler) Heartbeat() *pb.Heartbeat {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	metrics := readSystem(s.directory)
	heartbeat := &pb.Heartbeat{MemoryPercent: metrics.memory, DiskPercent: metrics.disk}
	if s.CollectorStatus != nil {
		heartbeat.CollectorStatus = s.CollectorStatus()
	}
	if s.TrafficSourceStatus != nil {
		heartbeat.TrafficSourceStatus = s.TrafficSourceStatus()
	}
	if now.Sub(s.sampled) >= time.Second && metrics.cpu.valid && s.previous.valid && metrics.cpu.total > s.previous.total && metrics.cpu.idle >= s.previous.idle {
		total := metrics.cpu.total - s.previous.total
		idle := metrics.cpu.idle - s.previous.idle
		if idle <= total {
			heartbeat.CpuPercent = percent(float64(total-idle) / float64(total) * 100)
		}
	}
	s.previous = metrics.cpu
	s.sampled = now
	return heartbeat
}
