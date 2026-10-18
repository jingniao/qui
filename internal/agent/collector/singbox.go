package collector

import (
	"errors"
	"net"
	"net/netip"
	"proxycontrol/pkg/contracts/access"
	policy "proxycontrol/pkg/contracts/policy"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SingboxWriter is the Agent's sing-box access source. It reads the core's own
// log stream, which the renderer raises to debug level while access collection
// is on, because that stream is the only upstream source that reports every
// connection:
//
//   - the Clash API `/connections` endpoint lists only the connections that are
//     still open (upstream trafficcontrol.Manager.Connections), so a rejected
//     connection - and any connection that opens and closes between two polls -
//     is never listed there;
//   - that metadata names no user (plan D5) and its `rule` field is a rendered
//     rule description, not the compiled rule index of this node;
//   - the log stream names the user of an authenticated inbound, the exact
//     destination, the compiled rule index and the outbound tag of every
//     connection, including rejected ones.
//
// The Clash API is still used, but only for the node byte totals of step 6.3.
//
// A separate writer belongs to each child process and its immutable compiler
// context. Only fixed native metadata messages are interpreted; raw logs and
// free-form errors are never published, persisted or passed to Agent's logger.
type SingboxWriter struct {
	mu         sync.Mutex
	context    *Context
	submit     func(access.Rollup) error
	reject     func()
	buffer     []byte
	discarding bool
	poisoned   bool
	now        func() time.Time
	pending    map[uint32]*singboxConnection
	lastSweep  time.Time
}

type singboxConnection struct {
	event      XrayEvent
	observedAt time.Time
	updated    time.Time
	routed     bool
	done       bool
}

var singboxHeader = regexp.MustCompile(`^[+-][0-9]{4} [0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} (INFO|DEBUG) \[([0-9]{1,10}) [^\]\r\n]{1,32}\] ([^:\r\n]{1,256}): (.*)$`)

func NewSingboxWriter(context *Context, submit func(access.Rollup) error, reject func()) *SingboxWriter {
	return &SingboxWriter{context: context, submit: submit, reject: reject, now: time.Now, pending: map[uint32]*singboxConnection{}}
}

func (w *SingboxWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, value := range data {
		if value == '\n' {
			if !w.discarding {
				w.line(strings.TrimSuffix(string(w.buffer), "\r"))
			}
			w.buffer = w.buffer[:0]
			w.discarding = false
			continue
		}
		if w.discarding {
			continue
		}
		if len(w.buffer) >= 8192 {
			w.buffer = w.buffer[:0]
			w.discarding = true
			w.poisoned = true
			w.failed()
			continue
		}
		w.buffer = append(w.buffer, value)
	}
	return len(data), nil
}

func (w *SingboxWriter) failed() {
	if w.reject != nil {
		w.reject()
	}
}

func SingboxRuleTag(index int) string { return "_native_rule_" + strconv.Itoa(index) }

func nativeTag(value, prefix string) (string, string, bool) {
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, "]") {
		return "", "", false
	}
	rest := strings.TrimPrefix(value, prefix)
	position := strings.IndexByte(rest, '[')
	if position < 1 {
		return "", "", false
	}
	protocol, id := rest[:position], rest[position+1:len(rest)-1]
	if protocol == "socks" {
		protocol = "socks5"
	}
	return protocol, id, tag(id)
}

func nativeDestination(value string) (string, string, int, error) {
	if strings.ContainsAny(value, " /?@#\\\t\r\n") || len(value) > 1024 {
		return "", "", 0, errors.New("invalid native access destination")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", "", 0, errors.New("native access destination requires authority")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", "", 0, errors.New("invalid native access port")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", "", 0, errors.New("native access zone is not supported")
		}
		return "", ip.Unmap().String(), port, nil
	}
	domain, err := policy.Domain(host, false)
	if err != nil {
		return "", "", 0, errors.New("invalid native access hostname")
	}
	return domain, "", port, nil
}

func (w *SingboxWriter) connection(id uint32, at time.Time) *singboxConnection {
	if at.Sub(w.lastSweep) >= 10*time.Second {
		for key, entry := range w.pending {
			if at.Sub(entry.updated) > 2*time.Minute {
				if !entry.done {
					w.failed()
				}
				delete(w.pending, key)
			}
		}
		w.lastSweep = at
	}
	if entry := w.pending[id]; entry != nil {
		return entry
	}
	if len(w.pending) >= 4096 {
		for key, entry := range w.pending {
			if at.Sub(entry.updated) > 2*time.Minute {
				if !entry.done {
					w.failed()
				}
				delete(w.pending, key)
			}
		}
	}
	if len(w.pending) >= 4096 {
		w.failed()
		return nil
	}
	entry := &singboxConnection{updated: at}
	w.pending[id] = entry
	return entry
}

func (w *SingboxWriter) line(line string) {
	if w.poisoned {
		return
	}
	match := singboxHeader.FindStringSubmatch(line)
	if match == nil {
		return
	}
	number, err := strconv.ParseUint(match[2], 10, 32)
	if err != nil {
		w.failed()
		return
	}
	id, logger, message := uint32(number), match[3], match[4]
	at := w.now().UTC()
	protocol, inboundID, inboundLogger := nativeTag(logger, "inbound/")
	if !inboundLogger {
		endpointProtocol, endpointID, endpointLogger := nativeTag(logger, "endpoint/")
		if endpointLogger && endpointProtocol == "wireguard" {
			if configured, known := w.context.Inbounds[endpointID]; known && configured.Protocol == "wireguard" {
				protocol, inboundID, inboundLogger = endpointProtocol, endpointID, true
			}
		}
	}
	if inboundLogger && match[1] == "INFO" {
		inbound, known := w.context.Inbounds[inboundID]
		if !known || protocol != inbound.Protocol {
			return
		}
		accountID := ""
		internalAccount := false
		if strings.HasPrefix(message, "[") {
			position := strings.Index(message, "] ")
			if position < 1 || position > 1025 {
				w.failed()
				return
			}
			name := message[1:position]
			message = message[position+2:]
			account, known := inbound.Accounts[name]
			if !known {
				w.failed()
				return
			}
			if account.Internal {
				internalAccount = true
			}
			accountID = account.ID
		}
		network := "tcp"
		destination := ""
		if strings.HasPrefix(message, "inbound connection to ") {
			destination = strings.TrimPrefix(message, "inbound connection to ")
		} else if strings.HasPrefix(message, "inbound packet connection to ") {
			network = "udp"
			destination = strings.TrimPrefix(message, "inbound packet connection to ")
		} else if message == "inbound packet connection" {
			network = "udp"
		} else {
			return
		}
		if accountID == "" && protocol == "shadowsocks" {
			// Legacy SS has one approved credential per listener. Never infer a
			// username for SOCKS UDP or other protocols from a source address.
			for _, account := range inbound.Accounts {
				if !account.Internal {
					if accountID != "" && accountID != account.ID {
						w.failed()
						return
					}
					accountID = account.ID
				}
			}
		}
		if accountID == "" && len(inbound.Accounts) > 0 && network == "tcp" {
			w.failed()
			return
		}
		event := XrayEvent{InboundID: inboundID, AccountID: accountID, Network: network}
		if destination != "" {
			event.Domain, event.TargetIP, event.Port, err = nativeDestination(destination)
			if err != nil {
				w.failed()
				// Native logs are line-oriented. A malformed destination may be
				// a truncated user-supplied newline; quarantine this process's
				// metadata stream instead of accepting injected subsequent lines.
				w.poisoned = true
				return
			}
		}
		if inbound.Internal || internalAccount {
			return
		}
		entry := w.connection(id, at)
		if entry == nil {
			return
		}
		if entry.event.InboundID != "" {
			if entry.event.InboundID != event.InboundID || entry.event.AccountID != event.AccountID || entry.event.Network != event.Network || entry.event.Domain != event.Domain || entry.event.TargetIP != event.TargetIP || entry.event.Port != event.Port {
				w.failed()
				entry.done = true
			}
			return
		}
		entry.event = event
		entry.observedAt = at
		entry.updated = at
		w.emit(entry, at)
		return
	}
	entry := w.pending[id]
	if entry == nil || entry.done {
		return
	}
	if logger == "router" && match[1] == "DEBUG" && strings.HasPrefix(message, "match[") {
		position := strings.IndexByte(message, ']')
		if position < 6 || position > 12 {
			w.failed()
			return
		}
		index, err := strconv.Atoi(message[len("match["):position])
		if err != nil || index < 0 {
			w.failed()
			return
		}
		origin, known := w.context.Exits[SingboxRuleTag(index)]
		if !known {
			w.failed()
			entry.done = true
			return
		}
		arrow := strings.LastIndex(message, " => ")
		if arrow < position {
			w.failed()
			return
		}
		if origin.Protocol == "_resolve_" {
			if message[arrow+len(" => "):] != "resolve("+origin.ID+")" {
				w.failed()
				entry.done = true
			}
			return
		}
		routeTag := origin.ID
		if origin.NativeTag != "" {
			routeTag = origin.NativeTag
		}
		expected := "route(" + routeTag + ")"
		if origin.Protocol == "block" {
			expected = "reject"
		}
		if message[arrow+len(" => "):] != expected {
			w.failed()
			entry.done = true
			return
		}
		entry.event.OutboundTag = SingboxRuleTag(index)
		entry.event.Rejected = origin.Protocol == "block"
		entry.routed = true
		entry.updated = at
		w.emit(entry, at)
		return
	}
	if protocol, outboundID, ok := nativeTag(logger, "outbound/"); ok && match[1] == "INFO" && entry.routed {
		origin := w.context.Exits[entry.event.OutboundTag]
		if origin.Protocol == EgressGroupProtocol {
			// The core chose the member itself (upstream balancer or urltest
			// group). Only the member the core names in its own log line is
			// accepted, and the record then reports that member's egress.
			member, known := w.context.Selections[entry.event.OutboundTag][outboundID]
			if !known {
				return
			}
			entry.event.SelectedOutbound = outboundID
			origin = member
		}
		expectedTag := origin.ID
		if origin.NativeTag != "" {
			expectedTag = origin.NativeTag
		}
		if expectedTag != outboundID || origin.Protocol != protocol {
			return
		}
		prefix := "outbound connection to "
		if entry.event.Network == "udp" {
			prefix = "outbound packet connection to "
		}
		if !strings.HasPrefix(message, prefix) {
			return
		}
		entry.event.Domain, entry.event.TargetIP, entry.event.Port, err = nativeDestination(strings.TrimPrefix(message, prefix))
		if err != nil {
			w.failed()
			entry.done = true
			w.poisoned = true
			return
		}
		w.emit(entry, at)
	}
}

func (w *SingboxWriter) emit(entry *singboxConnection, at time.Time) {
	if entry.done || !entry.routed || entry.event.InboundID == "" || entry.event.Port == 0 {
		return
	}
	if origin := w.context.Exits[entry.event.OutboundTag]; origin.Protocol == EgressGroupProtocol && origin.LogDomain && entry.event.SelectedOutbound == "" {
		return
	}
	entry.done = true
	if entry.observedAt.IsZero() {
		w.failed()
		return
	}
	// Routing/selection may arrive in a later minute. Count the connection
	// in its first validated inbound observation, not its delayed emission.
	// This is Agent observation time; native header times are not trusted.
	record, include, err := w.context.Record(entry.event, entry.observedAt)
	if err != nil {
		w.failed()
		return
	}
	if include && w.submit != nil {
		if err := w.submit(record); err != nil {
			w.failed()
		}
	}
}
