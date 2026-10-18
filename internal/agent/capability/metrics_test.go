package capability

import (
	"math"
	"runtime"
	"testing"
	"time"
)

func TestSamplerReportsRealResourcesAndUnknownValues(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("resource collection targets Linux and Windows")
	}
	sampler := NewSampler(t.TempDir())
	first := sampler.Heartbeat()
	if first.CpuPercent != nil {
		t.Fatal("reported CPU without a sampling window")
	}
	if first.MemoryPercent == nil || first.DiskPercent == nil {
		t.Fatal("memory or disk collection failed")
	}
	time.Sleep(1050 * time.Millisecond)
	next := sampler.Heartbeat()
	if next.CpuPercent == nil {
		t.Fatal("CPU sampling failed")
	}
	for _, value := range []*float64{next.CpuPercent, next.MemoryPercent, next.DiskPercent} {
		if value == nil || math.IsNaN(*value) || *value < 0 || *value > 100 {
			t.Fatal("invalid resource measurement")
		}
	}
	if percent(math.NaN()) != nil || percent(math.Inf(1)) != nil {
		t.Fatal("accepted nonfinite measurement")
	}
}
