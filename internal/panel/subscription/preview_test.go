package subscription

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

const previewUserID = "preview-user"
const previewDeviceID = "preview-device"
const previewCA = "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUQ2VydGlmaWNhdGU=\n-----END CERTIFICATE-----"

// previewUsers, previewNodes and previewPublished are the three interfaces the
// published source needs. A preview is a pure read, so the stubs are all the
// handler under test asks for.
type previewUsers struct {
	account     user.User
	device      user.Device
	credentials []user.Credential
}

func (p previewUsers) Get(context.Context, string) (user.User, error) { return p.account, nil }
func (p previewUsers) Device(context.Context, string) (user.Device, error) {
	return p.device, nil
}
func (p previewUsers) AccessIdentity(context.Context, string) (user.Device, error) {
	return p.device, nil
}
func (p previewUsers) Credentials(context.Context, string, bool) ([]user.Credential, error) {
	return p.credentials, nil
}

type previewNodes []node.Node

func (n previewNodes) ExpandedNodes(context.Context, []string) ([]node.Node, error) {
	return []node.Node(n), nil
}

// GroupIDsForNode reports the granted group, so the preview also proves the
// nodeGroup label the rule template expands later.
func (n previewNodes) UserInboundGroups(context.Context, string, string) ([]string, error) {
	return []string{"preview-group", "other-group"}, nil
}

type previewPublished map[string]desiredstate.State

func (p previewPublished) Published(_ context.Context, nodeID string) (desiredstate.State, error) {
	state, known := p[nodeID]
	if !known {
		return desiredstate.State{}, sql.ErrNoRows
	}
	return state, nil
}

// previewService is a user with two published endpoints: a VLESS entry that a URI
// can describe and a Hysteria2 entry that pins a CA, which the URI exporter
// refuses.
func previewService() *Service {
	account := user.User{ID: previewUserID, Status: "active", AllowedProtocols: []string{"vless", "hysteria2"}, NodeGroupIDs: []string{"preview-group"}}
	device := user.Device{ID: previewDeviceID, UserID: account.ID, Status: "active"}
	credentials := []user.Credential{
		{ID: "vless-credential", UserID: account.ID, DeviceID: device.ID, Protocol: "vless", Secret: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Status: "active"},
		{ID: "quic-credential", UserID: account.ID, DeviceID: device.ID, Protocol: "hysteria2", Secret: "quic-secret", Status: "active"},
	}
	users := previewUsers{account: account, device: device, credentials: credentials}
	n := node.Node{ID: "preview-node", Name: "Preview", Status: "online", Enabled: true, AllowSubscription: true, PublicIP: "node.example", Capabilities: &pb.Capabilities{Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Tcp: true}, {Protocol: "hysteria2", Inbound: true, Udp: true}}}}
	state := desiredstate.State{
		SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: []desiredstate.Credential{
			{ID: "vless-credential", UserID: account.ID, DeviceID: device.ID, Protocol: "vless", Secret: credentials[0].Secret},
			{ID: "quic-credential", UserID: account.ID, DeviceID: device.ID, Protocol: "hysteria2", Secret: credentials[1].Secret},
		},
		Inbounds: []desiredstate.Inbound{
			{ID: "vless-in", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{"vless-credential"}, DefaultOutbound: "direct"},
			{ID: "quic-in", Protocol: "hysteria2", Listen: "0.0.0.0", Port: 8443, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{"quic-credential"}, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Security: "tls", Transport: "quic", ServerName: "proxy.example", TrustedCAPEM: previewCA}},
		},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
	return &Service{
		Users:   users,
		Sources: source.NewRegistry(&published.Source{Users: users, Nodes: previewNodes{n}, Published: previewPublished{n.ID: state}}),
		Formats: DefaultFormats(),
	}
}

type previewBody struct {
	Items    []previewItem    `json:"items"`
	Total    int              `json:"total"`
	Warnings []previewWarning `json:"warnings"`
}

func readPreview(t *testing.T, handler *Handler, userID, deviceID string) (previewBody, []byte) {
	t.Helper()
	request := httptest.NewRequest("GET", "/api/v1/users/"+userID+"/subscription-preview?deviceId="+deviceID, nil)
	request.SetPathValue("id", userID)
	recorder := httptest.NewRecorder()
	handler.preview(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("preview: %d %s", recorder.Code, recorder.Body.String())
	}
	var body previewBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body, recorder.Body.Bytes()
}

// TestPreviewItemsCarryTheSubscriptionLink: the preview lists the endpoints the
// subscription would hold, and each URI-friendly item carries exactly the link
// the uri and Base64 formats would print for it, so the page can show and copy a
// working link instead of rebuilding one itself.
func TestPreviewItemsCarryTheSubscriptionLink(t *testing.T) {
	service := previewService()
	ctx := context.Background()
	proxies, _, err := service.Proxies(ctx, previewUserID, previewDeviceID)
	if err != nil || len(proxies) != 2 {
		t.Fatalf("preview endpoints: %d %v", len(proxies), err)
	}
	byInbound := map[string]proxy.Proxy{}
	for _, endpoint := range proxies {
		byInbound[endpoint.Origin.Ref] = endpoint
		if len(endpoint.Origin.Labels) != 1 || endpoint.Origin.Labels[0] != "nodeGroup:preview-group" {
			t.Fatalf("published group label %v", endpoint.Origin.Labels)
		}
	}
	body, raw := readPreview(t, &Handler{Service: service}, previewUserID, previewDeviceID)
	if body.Total != 2 || len(body.Items) != 2 {
		t.Fatalf("preview reports total %d with %d items", body.Total, len(body.Items))
	}
	// The preview is a link list: it carries no credential or settings field of
	// its own, so an added field cannot silently start leaking one.
	var document struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"nodeId": true, "nodeName": true, "inboundId": true, "protocol": true, "address": true, "port": true, "link": true}
	for _, item := range document.Items {
		for key := range item {
			if !allowed[key] {
				t.Fatalf("preview item grew an unexpected field %q: %s", key, raw)
			}
		}
	}
	items := map[string]previewItem{}
	for _, item := range body.Items {
		items[item.InboundID] = item
	}

	// The VLESS endpoint is described by the URI exporter, so the item repeats it
	// verbatim, next to the fields the page renders on its own.
	link, err := uri.Link(byInbound["vless-in"])
	if err != nil {
		t.Fatal(err)
	}
	vless := items["vless-in"]
	if vless.Link == nil || *vless.Link != link {
		t.Fatalf("preview link %v, want %q", vless.Link, link)
	}
	if vless.NodeID != "preview-node" || vless.NodeName != "Preview" || vless.Protocol != "vless" || vless.Address != "node.example" || vless.Port != 443 {
		t.Fatalf("preview item lost its fields: %+v", vless)
	}

	// The Hysteria2 endpoint pins a CA, which no URI can carry: the item keeps its
	// fields but must not grow an empty or unusable "link" key.
	if _, err := uri.Link(byInbound["quic-in"]); !errors.Is(err, uri.ErrUnrepresentable) {
		t.Fatalf("the fixture endpoint must be unrepresentable: %v", err)
	}
	quic := items["quic-in"]
	if quic.Link != nil {
		t.Fatalf("an unrepresentable endpoint got a link: %q", *quic.Link)
	}
	// The Owner is told why the endpoint is missing from the uri/base64 formats.
	if len(body.Warnings) != 1 || body.Warnings[0].Stage != "format" || !strings.Contains(body.Warnings[0].Message, "quic-in") {
		t.Fatalf("preview warnings %+v", body.Warnings)
	}
	if quic.NodeID != "preview-node" || quic.Protocol != "hysteria2" || quic.Address != "node.example" || quic.Port != 8443 {
		t.Fatalf("preview item lost its fields: %+v", quic)
	}
	var keys struct {
		Items []struct {
			InboundID string           `json:"inboundId"`
			Link      *json.RawMessage `json:"link"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, item := range keys.Items {
		if item.Link == nil && item.InboundID == "vless-in" {
			t.Fatalf("the VLESS item has no link key: %s", raw)
		}
		if item.Link != nil && item.InboundID == "quic-in" {
			t.Fatalf("the pinned-CA item has a link key: %s", raw)
		}
	}
}

// previewSettings answers the two settings reads auth.Service makes while it
// resolves a session.
type previewSettings struct{}

func (previewSettings) SaveSetup(context.Context, *sql.Tx, string, string) error { return nil }
func (previewSettings) ReadSetup(context.Context) (string, string, error) {
	return "Test Control", "UTC", nil
}

// The preview exposes a device's links and therefore stays with the Owner.
func TestPreviewRequiresOwner(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Settings: previewSettings{}, SessionDuration: time.Hour}
	handler := &Handler{Auth: &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	mux := http.NewServeMux()
	handler.Register(mux)

	now := time.Now().UTC()
	stamp := now.Format("2006-01-02T15:04:05.000000000Z")
	token := "viewer-session"
	if _, err := database.ExecContext(ctx, "INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES('admin-viewer','viewer','viewer','Viewer','active','unused',?)", stamp); err != nil {
		t.Fatal(err)
	}
	// The auth repository stores the SHA-256 of the cookie value.
	sum := sha256.Sum256([]byte(token))
	if _, err := database.ExecContext(ctx, "INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,'admin-viewer',?,?)", hex.EncodeToString(sum[:]), now.Add(time.Hour).Format("2006-01-02T15:04:05.000000000Z"), stamp); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/users/"+previewUserID+"/subscription-preview?deviceId="+previewDeviceID, nil)
	request.AddCookie(&http.Cookie{Name: "proxy_session", Value: token})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != 403 {
		t.Fatalf("viewer preview: %d %s", recorder.Code, recorder.Body.String())
	}
}
