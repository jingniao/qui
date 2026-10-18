package xray

import (
	"context"
	"errors"
	netproxy "golang.org/x/net/proxy"
	"net"
	"net/http"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/scheduler"
	"time"
)

func (a *Adapter) Probe(ctx context.Context) error {
	a.mu.Lock()
	probes := append([]core.Probe(nil), a.current.Probes...)
	known := map[string]scheduler.Health{}
	for id, status := range a.outboundHealth {
		known[id] = status
	}
	a.mu.Unlock()
	now := time.Now().UTC()
	var firstErr error
	for _, probe := range probes {
		if previous, exists := known[probe.OutboundID]; exists && now.Sub(previous.CheckedAt) < probe.Interval {
			continue
		}
		if err := scheduler.ValidateProbeURL(probe.URL); err != nil {
			return err
		}
		start := time.Now()
		status := scheduler.Health{CheckedAt: now}
		dialer, err := netproxy.SOCKS5("tcp", probe.Address, nil, &net.Dialer{Timeout: 4 * time.Second})
		if err == nil {
			contextDialer, ok := dialer.(netproxy.ContextDialer)
			if !ok {
				return errors.New("SOCKS health dialer lacks context")
			}
			transport := &http.Transport{DialContext: contextDialer.DialContext, DisableKeepAlives: true, TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 4 * time.Second}
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, probe.URL, nil)
			if requestErr == nil {
				response, responseErr := client.Do(request)
				if responseErr == nil {
					status.Healthy = response.StatusCode >= 200 && response.StatusCode < 400
					response.Body.Close()
				} else {
					err = responseErr
				}
			} else {
				err = requestErr
			}
			transport.CloseIdleConnections()
		}
		status.LatencyMS = time.Since(start).Milliseconds()
		a.mu.Lock()
		if a.outboundHealth == nil {
			a.outboundHealth = map[string]scheduler.Health{}
		}
		a.outboundHealth[probe.OutboundID] = status
		a.mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = errors.New("one or more outbound probes failed")
		}
	}
	return firstErr
}
func (a *Adapter) OutboundHealth() map[string]scheduler.Health {
	a.mu.Lock()
	defer a.mu.Unlock()
	copy := map[string]scheduler.Health{}
	active := map[string]bool{}
	for _, probe := range a.current.Probes {
		active[probe.OutboundID] = true
	}
	for id, value := range a.outboundHealth {
		if active[id] {
			copy[id] = value
		}
	}
	return copy
}
