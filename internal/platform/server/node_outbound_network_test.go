package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
)

func nodeAPIRequest(app *Server, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.1:123"
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	app.HTTP.Handler.ServeHTTP(response, request)
	return response
}

func TestNodeUpdateCarriesOutboundNetworkSettings(t *testing.T) {
	app := testServer(t)
	response := nodeAPIRequest(app, "POST", "/api/v1/auth/setup", `{"token":"`+app.SetupToken()+`","username":"owner","password":"node-test-password-123","platformName":"Test","timezone":"UTC"}`, nil, "")
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	var session auth.Session
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	response = nodeAPIRequest(app, "POST", "/api/v1/nodes/enrollment-token", `{"name":"Network node"}`, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var enrolled struct {
		Node node.Node
	}
	if err := json.Unmarshal(response.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/nodes/" + enrolled.Node.ID
	response = nodeAPIRequest(app, "PUT", path, `{"name":"Network node","enabled":true,"outboundSourceIp":" 192.0.2.10 ","outboundInterface":" eth0 ","hostSourceIpv4":"198.51.100.4","hostSourceIpv6":"2001:db8::4"}`, cookie, session.CSRFToken)
	if response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
	read := func() node.Node {
		t.Helper()
		response := nodeAPIRequest(app, "GET", path, "", cookie, "")
		if response.Code != 200 {
			t.Fatalf("node read failed: %d %s", response.Code, response.Body.String())
		}
		var stored node.Node
		if err := json.Unmarshal(response.Body.Bytes(), &stored); err != nil {
			t.Fatal(err)
		}
		return stored
	}
	stored := read()
	if stored.OutboundSourceIP != "192.0.2.10" || stored.OutboundInterface != "eth0" || stored.HostSourceIPv4 != "198.51.100.4" || stored.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("outbound network not stored or not returned trimmed: %+v", stored)
	}
	// An update that omits the settings keeps them.
	if response := nodeAPIRequest(app, "PUT", path, `{"name":"Renamed","enabled":true}`, cookie, session.CSRFToken); response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
	stored = read()
	if stored.Name != "Renamed" || stored.OutboundSourceIP != "192.0.2.10" || stored.OutboundInterface != "eth0" || stored.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("omitted settings were cleared: %+v", stored)
	}
	// Invalid values are refused with 400 before the repository runs.
	for _, body := range []string{
		`{"name":"Renamed","enabled":true,"outboundSourceIp":"127.0.0.1"}`,
		`{"name":"Renamed","enabled":true,"outboundSourceIp":"not-an-address"}`,
		`{"name":"Renamed","enabled":true,"outboundInterface":"eth0 1"}`,
		`{"name":"Renamed","enabled":true,"hostSourceIpv4":"2001:db8::4"}`,
		`{"name":"Renamed","enabled":true,"hostSourceIpv6":"198.51.100.4"}`,
	} {
		if response := nodeAPIRequest(app, "PUT", path, body, cookie, session.CSRFToken); response.Code != 400 {
			t.Fatalf("invalid settings accepted (%s): %d %s", body, response.Code, response.Body.String())
		}
	}
	stored = read()
	if stored.Name != "Renamed" || stored.OutboundSourceIP != "192.0.2.10" || stored.OutboundInterface != "eth0" || stored.HostSourceIPv4 != "198.51.100.4" || stored.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("rejected update changed the node: %+v", stored)
	}
}
