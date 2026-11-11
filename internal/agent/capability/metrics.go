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
	interfaces          map[string][2]uint64
	lastRates           map[string][2]float64
	lastCPU             *float64
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
	s.hostHeartbeat(heartbeat, now)
	if s.CollectorStatus != nil {
		heartbeat.CollectorStatus = s.CollectorStatus()
	}
	if s.TrafficSourceStatus != nil {
		heartbeat.TrafficSourceStatus = s.TrafficSourceStatus()
	}
	s.applyCPU(heartbeat, metrics.cpu, now)
	return heartbeat
}

// applyCPU fills the CPU percentage and advances the shared baseline (CPU
// counters and sampled time, also used by interfaceRates). A call less than a
// second after the baseline reuses the last computed value and leaves the
// baseline alone, so back-to-back heartbeats all carry a reading. It must run
// after interfaceRates, which reads the same baseline time.
func (s *Sampler) applyCPU(h *pb.Heartbeat, cur counters, now time.Time) {
	if now.Sub(s.sampled) < time.Second {
		h.CpuPercent = s.lastCPU
		return
	}
	s.lastCPU = nil
	if cur.valid && s.previous.valid && cur.total > s.previous.total && cur.idle >= s.previous.idle {
		total := cur.total - s.previous.total
		idle := cur.idle - s.previous.idle
		if idle <= total {
			s.lastCPU = percent(float64(total-idle) / float64(total) * 100)
		}
	}
	h.CpuPercent = s.lastCPU
	s.previous = cur
	s.sampled = now
}
