package collector

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	policy "proxycontrol/pkg/contracts/policy"
	"strconv"
	"strings"
)

// XrayEvent contains only allowlisted metadata. Raw stdout, source addresses
// and free-form rejection reasons must never be persisted or logged by Agent.
type XrayEvent struct {
	Domain, TargetIP                           string
	Port                                       int
	Network, InboundID, OutboundTag, AccountID string
	Rejected                                   bool
	SelectedOutbound                           string
}

func ParseXrayAccessLine(line string) (XrayEvent, error) {
	event, err := parseXrayAccessLine(line)
	if err != nil && event.Rejected {
		// A rejection may carry no destination at all, for example a failed
		// inbound handshake:
		//
		//	from tcp:127.0.0.1:63178 rejected  proxy/socks: invalid username or password
		//
		// The free-form reason text is never parsed, so such a line becomes a
		// rejection without a domain or address. The caller counts it as an
		// unattributed observation instead of inventing a destination.
		return XrayEvent{Rejected: true}, nil
	}
	return event, err
}

func parseXrayAccessLine(line string) (XrayEvent, error) {
	var event XrayEvent
	if len(line) > 8192 || strings.ContainsAny(line, "\r\n") {
		return event, errors.New("invalid access line")
	}
	marker := " accepted "
	position := strings.Index(line, marker)
	if position < 0 {
		marker = " rejected "
		position = strings.Index(line, marker)
		if position >= 0 {
			event.Rejected = true
		}
	}
	if position < 0 {
		return event, errors.New("not an access event")
	}
	rest := strings.TrimLeft(line[position+len(marker):], " ")
	if rest == "" {
		return event, errors.New("access event has no destination")
	}
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		return event, errors.New("access event has no route metadata")
	}
	target := rest[:end]
	if strings.HasPrefix(target, "tcp:") {
		event.Network = "tcp"
		target = strings.TrimPrefix(target, "tcp:")
	} else if strings.HasPrefix(target, "udp:") {
		event.Network = "udp"
		target = strings.TrimPrefix(target, "udp:")
	} else {
		// HTTP's access message uses a URL or CONNECT authority rather than
		// net.Destination. Extract only the authority, never path/query/userinfo.
		event.Network = "tcp"
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "//") {
			parsed, err := url.Parse(target)
			if err != nil || parsed.User != nil || parsed.Hostname() == "" {
				return event, errors.New("invalid HTTP access authority")
			}
			port := parsed.Port()
			if port == "" {
				port = "80"
				if parsed.Scheme == "https" {
					port = "443"
				}
			}
			target = net.JoinHostPort(parsed.Hostname(), port)
		}
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return event, errors.New("invalid access target")
	}
	event.Port, err = strconv.Atoi(port)
	if err != nil || event.Port < 1 || event.Port > 65535 {
		return event, errors.New("invalid access port")
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Zone() == "" {
		event.TargetIP = ip.Unmap().String()
	} else {
		domain, err := policy.Domain(host, false)
		if err != nil {
			return event, errors.New("invalid access domain")
		}
		event.Domain = domain
	}
	routeStart := strings.Index(rest[end:], "[")
	if routeStart < 0 {
		return event, errors.New("access event has no route tags")
	}
	routeStart += end
	routeEnd := strings.Index(rest[routeStart:], "]")
	if routeEnd < 0 {
		return event, errors.New("invalid access route tags")
	}
	routeEnd += routeStart
	tags := strings.Fields(rest[routeStart+1 : routeEnd])
	if len(tags) != 3 || tags[1] != "->" && tags[1] != "==>" && tags[1] != ">>" {
		return event, errors.New("unsupported access route format")
	}
	if !tag(tags[0]) || !tag(tags[2]) {
		return event, errors.New("invalid access route identifier")
	}
	event.InboundID, event.OutboundTag = tags[0], tags[2]
	if suffix := rest[routeEnd+1:]; strings.Contains(suffix, " email: ") {
		account := strings.TrimSpace(suffix[strings.LastIndex(suffix, " email: ")+len(" email: "):])
		account = strings.TrimPrefix(account, "credential:")
		if !tag(account) {
			return event, errors.New("invalid access account identity")
		}
		event.AccountID = account
	}
	return event, nil
}

func tag(value string) bool {
	if value == "" || len(value) > 1024 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
