package connections

import (
	"proxycontrol/pkg/contracts/traffic"
	"sort"
	"sync"
	"time"
)

const ActiveWindow = 60 * time.Second
const ReleaseWindow = 120 * time.Second

// Admission owns only volatile address state. Callers pass already authorized
// entry-user observations; relay identities must never be supplied.
type Admission struct {
	Now    func() time.Time
	mu     sync.Mutex
	users  map[string]*admittedUser
	serial uint64
}
type admittedUser struct {
	keys  map[string]*admittedKey
	known time.Time
}
type admittedKey struct {
	order       uint64
	first, last time.Time
	allowed     bool
	nodes       map[string]time.Time
}
type DeviceView struct {
	Address   string    `json:"address"`
	Allowed   bool      `json:"allowed"`
	Active    bool      `json:"active"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Nodes     []string  `json:"nodes"`
}

func (a *Admission) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Observe is called once per fresh snapshot, never from a view read. Empty
// measured reports preserve known-zero semantics without extending key life.
func (a *Admission) Observe(userID, nodeID string, keys []string, internal bool) {
	if internal {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.users == nil {
		a.users = map[string]*admittedUser{}
	}
	u := a.users[userID]
	if u == nil {
		u = &admittedUser{keys: map[string]*admittedKey{}}
		a.users[userID] = u
	}
	now := a.now()
	u.known = now
	for _, raw := range keys {
		key, ok := traffic.AddressKey(raw)
		if !ok {
			continue
		}
		v := u.keys[key]
		if v == nil {
			a.serial++
			v = &admittedKey{order: a.serial, first: now, nodes: map[string]time.Time{}}
			u.keys[key] = v
		}
		v.last = now
		v.nodes[nodeID] = now
	}
}
func ordered(u *admittedUser) []string {
	keys := make([]string, 0, len(u.keys))
	for key := range u.keys {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return u.keys[keys[i]].order < u.keys[keys[j]].order })
	return keys
}
func (a *Admission) recompute(u *admittedUser, limit int, now time.Time) {
	for key, v := range u.keys {
		if now.Sub(v.last) >= ActiveWindow+ReleaseWindow {
			delete(u.keys, key)
		}
	}
	keys := ordered(u)
	slots := limit
	for _, key := range keys {
		v := u.keys[key]
		if limit == 0 {
			v.allowed = true
		} else if v.allowed {
			if slots > 0 {
				slots--
			} else {
				v.allowed = false
			}
		}
	}
	if limit == 0 {
		return
	}
	for _, key := range keys {
		v := u.keys[key]
		if !v.allowed && slots > 0 && now.Sub(v.last) < ActiveWindow {
			v.allowed = true
			slots--
		}
	}
}

// State returns masked, stable-order views plus private gate keys. Private keys
// are for transport only and must never enter audit, logging or storage paths.
func (a *Admission) State(userID string, limit int) ([]DeviceView, []string, *int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.users[userID]
	if u == nil {
		return []DeviceView{}, nil, nil
	}
	now := a.now()
	a.recompute(u, limit, now)
	views := []DeviceView{}
	gates := []string{}
	count := 0
	for _, key := range ordered(u) {
		v := u.keys[key]
		active := now.Sub(v.last) < ActiveWindow
		if active {
			count++
		}
		nodes := []string{}
		for n, at := range v.nodes {
			if now.Sub(at) < ActiveWindow {
				nodes = append(nodes, n)
			}
		}
		sort.Strings(nodes)
		views = append(views, DeviceView{Address: traffic.MaskAddress(key), Allowed: v.allowed, Active: active, FirstSeen: v.first, LastSeen: v.last, Nodes: nodes})
		if !v.allowed && active && limit > 0 {
			gates = append(gates, key)
		}
	}
	if now.Sub(u.known) >= ActiveWindow {
		return views, gates, nil
	}
	return views, gates, &count
}
func (a *Admission) Reset(userID string) { a.mu.Lock(); defer a.mu.Unlock(); delete(a.users, userID) }
