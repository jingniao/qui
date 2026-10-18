package xray

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func groupRequest(t *testing.T, port int, target string) (string, bool) {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	targetPort, _ := strconv.Atoi(parsed.Port())
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	connection.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return "", false
	}
	request := binary.BigEndian.AppendUint16([]byte{5, 1, 0, 1, 127, 0, 0, 1}, uint16(targetPort))
	connection.Write(request)
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		return "", false
	}
	io.WriteString(connection, "GET /data HTTP/1.0\r\nHost: probe\r\n\r\n")
	data, err := io.ReadAll(connection)
	return string(data), err == nil && len(data) > 0
}
func httpProxyServer(t *testing.T, name string, delay time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		if r.Method == http.MethodConnect {
			hijacker := w.(http.Hijacker)
			connection, buffered, err := hijacker.Hijack()
			if err != nil {
				return
			}
			defer connection.Close()
			connection.SetDeadline(time.Now().Add(3 * time.Second))
			io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n\r\n")
			request, err := http.ReadRequest(buffered.Reader)
			if err != nil {
				return
			}
			request.Body.Close()
			io.WriteString(connection, "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Length: "+strconv.Itoa(len(name))+"\r\n\r\n"+name)
			return
		}
		io.WriteString(w, name)
	}))
	t.Cleanup(server.Close)
	return server
}
func TestRealOutboundGroupsProbeSelectionAndConnectionWeights(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for real group scheduler integration")
	}
	for _, strategy := range []string{"primary", "round_robin", "latency"} {
		t.Run(strategy, func(t *testing.T) {
			fast := httpProxyServer(t, "egress-fast", 0)
			slow := httpProxyServer(t, "egress-slow", 40*time.Millisecond)
			fastURL, _ := url.Parse(fast.URL)
			slowURL, _ := url.Parse(slow.URL)
			fastPort, _ := strconv.Atoi(fastURL.Port())
			slowPort, _ := strconv.Atoi(slowURL.Port())
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-origin") }))
			defer origin.Close()
			state := testState(availablePort(t), 1)
			state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: "fast", Protocol: "http", Address: "127.0.0.1", Port: fastPort, Enabled: true}, desiredstate.Outbound{ID: "slow", Protocol: "http", Address: "127.0.0.1", Port: slowPort, Enabled: true})
			state.OutboundGroups = []desiredstate.OutboundGroup{{ID: "egress", Strategy: strategy, ProbeURL: origin.URL, ProbeInterval: 3, Failover: "Block", Members: []desiredstate.GroupMember{{OutboundID: "slow", Priority: 1, Weight: 2}, {OutboundID: "fast", Priority: 2, Weight: 1}}}}
			state.Inbounds[0].DefaultOutbound = "egress"
			adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID}
			defer adapter.Close()
			manager, err := reconcile.New(t.TempDir(), state.NodeID, adapter)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			if _, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL); allowed {
				t.Fatal("unknown health group failed open")
			}
			if err := adapter.Probe(ctx); err != nil {
				t.Fatal(err)
			}
			if changed, err := manager.Refresh(ctx); err != nil || !changed {
				t.Fatal("probe results not applied", err)
			}
			counts := map[string]int{}
			for index := 0; index < 6; index++ {
				body, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL)
				if !allowed {
					t.Fatal("healthy group blocked")
				}
				if strings.Contains(body, "egress-fast") {
					counts["fast"]++
				} else if strings.Contains(body, "egress-slow") {
					counts["slow"]++
				} else {
					t.Fatal("request bypassed actual group egress")
				}
			}
			switch strategy {
			case "primary":
				if counts["slow"] != 6 {
					t.Fatal("primary not preferred", counts)
				}
			case "latency":
				if counts["fast"] != 6 {
					t.Fatal("fastest not preferred", counts)
				}
			case "round_robin":
				if counts["fast"] != 3 || counts["slow"] != 3 {
					t.Fatal("not per-connection round robin", counts)
				}
			}
			slow.Close()
			time.Sleep(3050 * time.Millisecond)
			adapter.Probe(ctx)
			if _, err := manager.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			body, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL)
			if !allowed || !strings.Contains(body, "egress-fast") {
				t.Fatal("failed member not removed")
			}
			fast.Close()
			time.Sleep(3050 * time.Millisecond)
			adapter.Probe(ctx)
			if _, err := manager.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			if _, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL); allowed {
				t.Fatal("all failed group fell through to Direct")
			}
		})
	}
}
