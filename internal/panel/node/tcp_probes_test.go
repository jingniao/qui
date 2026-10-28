package node

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strconv"
	"testing"
	"time"
)

func TestTCPProbeDispatchResultBindingAndExpiry(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(ctx, "probe", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO probe_targets VALUES('target','test','127.0.0.1',443,1)"); err != nil {
		t.Fatal(err)
	}
	c := &pb.TCPProbeCommand{Id: "request", Addresses: []string{"127.0.0.1"}, Port: 443, Attempts: 3, TimeoutMs: 100, AllowNonPublic: true, Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	raw, _ := json.Marshal(c)
	if _, err = database.Exec("INSERT INTO node_tcp_probes VALUES(?,?,?, ?,NULL,?,?,'queued')", c.Id, n.ID, "target", string(raw), timestamp(time.Now()), timestamp(c.Deadline.AsTime())); err != nil {
		t.Fatal(err)
	}
	command, err := r.pendingTCPProbe(ctx, n.ID)
	if err != nil || command == nil || command.Addresses[0] != "127.0.0.1" {
		t.Fatal(command, err)
	}
	result := &pb.TCPProbeResult{Id: c.Id, Attempted: 3, Lost: 3, Code: "complete"}
	if r.recordTCPProbe(ctx, "foreign", result) == nil {
		t.Fatal("foreign result accepted")
	}
	result.Attempted = 4
	if r.recordTCPProbe(ctx, n.ID, result) == nil {
		t.Fatal("bad counts accepted")
	}
	result.Attempted = 3
	if err = r.recordTCPProbe(ctx, n.ID, result); err != nil {
		t.Fatal(err)
	}
	if err = r.recordTCPProbe(ctx, n.ID, result); err != nil {
		t.Fatal("duplicate", err)
	}
	command, err = r.pendingTCPProbe(ctx, n.ID)
	if err != nil || command != nil {
		t.Fatal(command, err)
	}
	database.Exec("UPDATE node_tcp_probes SET status='queued',deadline=?", timestamp(time.Now().Add(-time.Second)))
	command, err = r.pendingTCPProbe(ctx, n.ID)
	if err != nil || command != nil {
		t.Fatal(command, err)
	}
}

func TestPanelReachabilityTCPAndTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	tlsPort, _ := strconv.Atoi(portText)
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	if err := PanelReachability(context.Background(), host, tlsPort, "", true, cert); err != nil {
		t.Fatal("configured certificate rejected", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	if err = PanelReachability(context.Background(), "127.0.0.1", port, "", false); err != nil {
		t.Fatal(err)
	}
	if err = PanelReachability(context.Background(), "127.0.0.1", port, "localhost", true); err == nil {
		t.Fatal("plain TCP accepted as TLS")
	}
	l.Close()
	if err = PanelReachability(context.Background(), "127.0.0.1", port, "", false); err == nil {
		t.Fatal("closed port reachable")
	}
}
