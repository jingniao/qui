package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/testutil"
)

// productionPanel builds a serving panel with the test TLS material, so the
// trust anchor endpoint has a real certificate chain to publish.
func productionPanel(t *testing.T) *Server {
	t.Helper()
	previous := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = previous })
	app := testServer(t)
	c := app.Config
	c.Development = false
	c.GRPCAddress = "127.0.0.1:19443"
	c.TLS = testutil.Certificates(t).Server
	secured, err := New(c, app.Database, fstest.MapFS{"index.html": {Data: []byte("UI")}}, app.Logger)
	if err != nil {
		t.Fatal(err)
	}
	return secured
}

func trustAnchor(t *testing.T, app *Server) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	app.HTTP.Handler.ServeHTTP(response, httptest.NewRequest("GET", "/install/ca.pem", nil))
	return response
}

func TestInstallCAEndpointServesTheTopCertificateOfTheChain(t *testing.T) {
	app := productionPanel(t)
	leaf, err := os.ReadFile(app.Config.TLS.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := os.ReadFile(app.Config.TLS.ClientCA)
	if err != nil {
		t.Fatal(err)
	}
	chain := filepath.Join(t.TempDir(), "chain.pem")
	if err := os.WriteFile(chain, append(append([]byte{}, leaf...), anchor...), 0600); err != nil {
		t.Fatal(err)
	}
	app.Config.TLS.Certificate = chain

	response := trustAnchor(t, app)
	if response.Code != 200 {
		t.Fatalf("trust anchor: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/x-pem-file" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected headers: %v", response.Header())
	}
	if response.Body.String() != string(anchor) {
		t.Fatal("the endpoint did not serve the last certificate of the chain")
	}
	digest := sha256.Sum256(anchor)
	if info := app.installInfo(); info.Version != "1.2.3" || info.GRPCPort != 19443 || info.CASHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("install info does not describe the served bytes: %+v", info)
	}
}

func TestInstallCAEndpointRefusesWithoutAReadableCertificate(t *testing.T) {
	development := testServer(t)
	if response := trustAnchor(t, development); response.Code != 404 {
		t.Fatalf("development mode served a trust anchor: %d", response.Code)
	}
	app := productionPanel(t)
	directory := t.TempDir()
	unparsable := filepath.Join(directory, "unparsable.pem")
	if err := os.WriteFile(unparsable, []byte("not a certificate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing certificate": filepath.Join(directory, "missing.pem"), "unparsable certificate": unparsable} {
		app.Config.TLS.Certificate = path
		if response := trustAnchor(t, app); response.Code != 404 {
			t.Fatalf("%s served a trust anchor: %d", name, response.Code)
		}
		if info := app.installInfo(); info.CASHA256 != "" {
			t.Fatalf("%s produced a digest: %+v", name, info)
		}
	}
}

func TestEnrollmentResponseCarriesInstallInfo(t *testing.T) {
	app := productionPanel(t)
	response := nodeAPIRequest(app, "POST", "/api/v1/auth/setup", `{"token":"`+app.SetupToken()+`","username":"owner","password":"node-test-password-123","platformName":"Test","timezone":"UTC"}`, nil, "")
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	var session auth.Session
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	response = nodeAPIRequest(app, "POST", "/api/v1/nodes/enrollment-token", `{"name":"Install node"}`, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var enrolled struct {
		Node    node.Node
		Install node.InstallInfo
	}
	if err := json.Unmarshal(response.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	if enrolled.Node.Status != "pending" {
		t.Fatalf("enrollment failed: %+v", enrolled.Node)
	}
	digest := sha256.Sum256(trustAnchor(t, app).Body.Bytes())
	if enrolled.Install.Version != "1.2.3" || enrolled.Install.GRPCPort != 19443 || enrolled.Install.CASHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("install info does not match the trust anchor: %+v", enrolled.Install)
	}
}
