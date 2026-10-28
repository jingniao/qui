package subscription

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/platform/vault"
)

// A6.6 (the endpoint half): the Owner can view the link again after 0114, every
// registered format gets its own URL from the server, a pre-1.1 token reports
// link_not_recoverable, and a user without a token is not found.
func TestSubscriptionLinkEndpoint(t *testing.T) {
	p := newRulesPanel(t)
	configurationVault, err := vault.Open(filepath.Join(t.TempDir(), "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	p.service.Repository.Vault = configurationVault
	owner, _ := sessionFor(t, p.database, "Owner")
	path := "/api/v1/users/" + p.u.ID + "/subscription-link"

	response := p.do(t, "GET", path, "", owner, "")
	if response.Code != 404 || !strings.Contains(response.Body.String(), "not_found") {
		t.Fatalf("no token: %d %s", response.Code, response.Body.String())
	}

	_, plaintext, err := p.service.Rotate(context.Background(), p.u.ID, p.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	response = p.do(t, "GET", path, "", owner, "")
	var body struct {
		Path    string
		Formats []struct{ ID, URL string }
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil {
		t.Fatalf("link: %d %s", response.Code, response.Body.String())
	}
	if body.Path != "/sub/"+plaintext {
		t.Fatalf("path %q does not carry the active token", body.Path)
	}
	wanted := map[string]bool{"uri": false, "base64": false, "sing-box": false, "clash": false}
	for _, item := range body.Formats {
		if _, ok := wanted[item.ID]; ok {
			wanted[item.ID] = true
		}
		if item.URL != body.Path+"?format="+item.ID {
			t.Fatalf("format %q has URL %q", item.ID, item.URL)
		}
	}
	for id, seen := range wanted {
		if !seen {
			t.Fatalf("format %q missing from the link response: %+v", id, body.Formats)
		}
	}

	// What a token issued before v1.1 looks like.
	if _, err := p.database.Exec("UPDATE subscription_tokens SET encrypted_token=NULL WHERE user_id=? AND revoked_at IS NULL", p.u.ID); err != nil {
		t.Fatal(err)
	}
	response = p.do(t, "GET", path, "", owner, "")
	if response.Code != 404 || !strings.Contains(response.Body.String(), "link_not_recoverable") {
		t.Fatalf("pre-1.1 token: %d %s", response.Code, response.Body.String())
	}

	if response := p.do(t, "GET", "/api/v1/users/missing/subscription-link", "", owner, ""); response.Code != 404 {
		t.Fatalf("unknown user: %d", response.Code)
	}
	if response := p.do(t, "GET", path, "", nil, ""); response.Code != 401 && response.Code != 403 {
		t.Fatalf("anonymous caller: %d", response.Code)
	}
}
