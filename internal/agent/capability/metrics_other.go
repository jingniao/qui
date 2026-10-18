//go:build !linux && !windows

package capability

func readSystem(string) systemMetrics { return systemMetrics{} }
