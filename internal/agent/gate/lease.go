package gate

import (
	"proxycontrol/pkg/contracts/devicegate"
	"time"
)

// Lease is owned under the core adapter mutex and is never written to disk.
type Lease struct {
	request devicegate.Request
	until   time.Time
}

func (l *Lease) Set(data []byte, now time.Time) error {
	r, err := devicegate.Decode(data)
	if err != nil {
		l.request = devicegate.Request{}
		l.until = time.Time{}
		return err
	}
	l.request = r
	l.until = now.Add(time.Duration(r.TTLSeconds) * time.Second)
	return nil
}
func (l *Lease) Entries(now time.Time) []devicegate.Entry {
	if !now.Before(l.until) {
		l.request = devicegate.Request{}
		return nil
	}
	return l.request.Entries
}
func (l *Lease) Clear() { l.request = devicegate.Request{}; l.until = time.Time{} }
