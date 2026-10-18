package core

import (
	"context"
	"errors"
	netproxy "golang.org/x/net/proxy"
	"net"
	"net/http"
	"proxycontrol/pkg/contracts/scheduler"
	"strconv"
	"time"
)

// ObserveProbe dials only the pinned core's private SOCKS entry. Redirects and
// response bodies are not consumed or retained; endpoint choice stays in core.
func ObserveProbe(ctx context.Context, probe Probe) (scheduler.Health, error) {
	status := scheduler.Health{CheckedAt: time.Now().UTC()}
	host, portText, err := net.SplitHostPort(probe.Address)
	port, portErr := strconv.Atoi(portText)
	if err != nil || host != "127.0.0.1" || portErr != nil || port < 1 || port > 65535 {
		return status, errors.New("core probe endpoint must be private loopback")
	}
	if err := scheduler.ValidateProbeURL(probe.URL); err != nil {
		return status, err
	}
	start := time.Now()
	dialer, err := netproxy.SOCKS5("tcp", probe.Address, nil, &net.Dialer{Timeout: 4 * time.Second})
	if err != nil {
		return status, errors.New("core outbound probe failed")
	}
	contextDialer, ok := dialer.(netproxy.ContextDialer)
	if !ok {
		return status, errors.New("core probe lacks context dialer")
	}
	transport := &http.Transport{DialContext: contextDialer.DialContext, DisableKeepAlives: true, TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 4 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.URL, nil)
	if err == nil {
		response, responseErr := client.Do(request)
		if responseErr == nil {
			status.Healthy = response.StatusCode >= 200 && response.StatusCode < 400
			response.Body.Close()
		} else {
			err = responseErr
		}
	}
	status.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		return status, errors.New("core outbound probe failed")
	}
	return status, nil
}
