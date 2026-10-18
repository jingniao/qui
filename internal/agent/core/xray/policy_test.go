package xray

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func domainRequest(t *testing.T, port int, domain string, targetPort int) (string, bool) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	connection.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil {
		t.Fatal(err)
	}
	message := append([]byte{5, 1, 0, 3, byte(len(domain))}, []byte(domain)...)
	message = binary.BigEndian.AppendUint16(message, uint16(targetPort))
	connection.Write(message)
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		return "", false
	}
	io.WriteString(connection, "GET / HTTP/1.0\r\nHost: "+domain+"\r\n\r\n")
	data, err := io.ReadAll(connection)
	return string(data), err == nil && len(data) > 0
}
func TestRealPolicyDomainAndCIDRBlockWithDefaultDirect(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for live policy blocking")
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "policy-default-direct-origin") }))
	defer origin.Close()
	originPort := origin.Listener.Addr().(*net.TCPAddr).Port
	port := availablePort(t)
	state := testState(port, 1)
	state.Policies = []desiredstate.Policy{{ID: "domain-block", Name: "Domain block", Priority: 10, Enabled: true, Match: desiredstate.Match{Domains: []string{"*.blocked.test"}}, Action: desiredstate.Action{Type: "Block"}}}
	startState(t, state)
	if _, allowed := domainRequest(t, port, "api.blocked.test", originPort); allowed {
		t.Fatal("domain block allowed traffic")
	}
	if !strings.Contains(socksRequest(t, port, origin.URL), "policy-default-direct-origin") {
		t.Fatal("default direct stopped working")
	}
	state.Revision = 2
	state.Policies[0].Match = desiredstate.Match{CIDRs: []string{"127.0.0.0/8"}}
	rendered, err := Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	var document configDocument
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Routing.Rules) < 2 || document.Routing.Rules[0].IP[0] != "127.0.0.0/8" {
		t.Fatal("CIDR rule not compiled")
	}
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID}
	defer adapter.Close()
	otherPort := availablePort(t)
	state.Inbounds[0].Port = otherPort
	rendered, err = Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Apply(context.Background(), rendered); err != nil {
		t.Fatal(err)
	}
	if _, allowed := domainRequest(t, otherPort, "127.0.0.1", originPort); allowed {
		t.Fatal("CIDR block allowed local target")
	}
}
func authRequest(t *testing.T, port int, username, password string, origin *httptest.Server) bool {
	t.Helper()
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	connection.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 2 {
		t.Fatal("auth negotiation failed", err)
	}
	auth := append([]byte{1, byte(len(username))}, []byte(username)...)
	auth = append(auth, byte(len(password)))
	auth = append(auth, []byte(password)...)
	connection.Write(auth)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		t.Fatal("authentication failed", err)
	}
	targetPort := origin.Listener.Addr().(*net.TCPAddr).Port
	connection.Write(binary.BigEndian.AppendUint16([]byte{5, 1, 0, 1, 127, 0, 0, 1}, uint16(targetPort)))
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		return false
	}
	io.WriteString(connection, "GET / HTTP/1.0\r\nHost: origin\r\n\r\n")
	data, err := io.ReadAll(connection)
	return err == nil && strings.Contains(string(data), "scoped-policy-origin")
}
func TestRealDeviceScopedPolicyDoesNotBlockOtherDevice(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for credential-scoped policy")
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "scoped-policy-origin") }))
	defer origin.Close()
	state := testState(availablePort(t), 1)
	first := desiredstate.Credential{ID: "device-one", UserID: "user-one", DeviceID: "one", Protocol: "socks5", Username: "one", Secret: "first-device-secret"}
	second := first
	second.ID = "device-two"
	second.DeviceID = "two"
	second.Username = "two"
	second.Secret = "second-device-secret"
	state.Users = []desiredstate.User{{ID: "user-one", Enabled: true}}
	state.Credentials = []desiredstate.Credential{first, second}
	state.Inbounds[0].CredentialIDs = []string{first.ID, second.ID}
	state.Policies = []desiredstate.Policy{{ID: "block-one", Name: "Device one", Priority: 1, Enabled: true, Scope: desiredstate.Scope{UserIDs: []string{"user-one"}, DeviceIDs: []string{"one"}}, Match: desiredstate.Match{Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}}}
	startState(t, state)
	if authRequest(t, state.Inbounds[0].Port, first.Username, first.Secret, origin) {
		t.Fatal("selected device policy bypassed")
	}
	if !authRequest(t, state.Inbounds[0].Port, second.Username, second.Secret, origin) {
		t.Fatal("other device was incorrectly blocked")
	}
}
