package collector

import "time"

func (w *SingboxWriter) rememberSource(entry *singboxConnection, now time.Time) {
	if entry.source == "" || entry.event.AccountID == "" {
		return
	}
	if len(w.sources) >= 65536 {
		return
	}
	previous, exists := w.sources[entry.source]
	credential := entry.event.AccountID
	if exists && previous.credential != credential {
		credential = ""
	}
	w.sources[entry.source] = singboxSource{credential: credential, updated: now}
}

// SourceUsers joins only the currently open Clash source IP+port tuples. This
// volatile map is separate from access rollups, which never contain sources.
func (w *SingboxWriter) SourceUsers(active map[string]bool) (map[string]string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := map[string]string{}
	if w.poisoned {
		return result, false
	}
	now := w.now()
	for source, v := range w.sources {
		if active[source] {
			v.updated = now
			w.sources[source] = v
			if v.credential != "" {
				result[source] = v.credential
			}
		} else if now.Sub(v.updated) >= 2*time.Minute {
			delete(w.sources, source)
		}
	}
	return result, true
}
