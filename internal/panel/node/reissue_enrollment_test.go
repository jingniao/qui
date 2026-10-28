package node

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
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/db"
	"strings"
	"testing"
	"time"
)

// reissueAudit adds the non-transactional Record the re-issue handler writes
// through to the transactional writer the deletion tests already provide.
type reissueAudit struct{ testAudit }

func (a reissueAudit) Record(ctx context.Context, actor, action, target, result, ip string) error {
	_, err := a.database.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,?,?)", time.Now().UTC().Format(time.RFC3339Nano), actor, action, target, result, ip)
	return err
}

// sessionSettings answers the two settings reads auth.Service makes while it
// resolves a session; no platform setting takes part in these tests.
type sessionSettings struct{}

func (sessionSettings) SaveSetup(context.Context, *sql.Tx, string, string) error { return nil }
func (sessionSettings) ReadSetup(context.Context) (string, string, error) {
	return "Test Control", "UTC", nil
}

// digest is the form the auth repository stores: the SHA-256 of the token as
// lowercase hex. A session row is seeded with it because the plaintext token is
// all a browser ever holds, and the CSRF token is the same digest of "csrf:"
// followed by the token.
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// reissuePanel is the smallest panel the re-issue route needs: the node module,
// a real operation log and a real session lookup, reached through the router so
// the route pattern and the Owner requirement are exercised as well.
type reissuePanel struct {
	mux        *http.ServeMux
	repository *Repository
	database   *sql.DB
}

func newReissuePanel(t *testing.T) *reissuePanel {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	service := &auth.Service{Repository: &auth.Repository{Database: database}, Settings: sessionSettings{}, SessionDuration: time.Hour}
	handler := &Handler{
		Repository: &Repository{Database: database},
		Audit:      reissueAudit{testAudit{database: database}},
		Auth:       &auth.Handler{Service: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	return &reissuePanel{mux: mux, repository: handler.Repository, database: database}
}

// session seeds one administrator with a live session and returns the cookie and
// CSRF token that administrator's browser would send.
func (p *reissuePanel) session(t *testing.T, role string) (*http.Cookie, string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	stamp := now.Format("2006-01-02T15:04:05.000000000Z")
	token := "session-" + strings.ToLower(role)
	if _, err := p.database.ExecContext(ctx, "INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,'active','unused',?)", role, role, role, role, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.ExecContext(ctx, "INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", digest(token), role, now.Add(time.Hour).Format("2006-01-02T15:04:05.000000000Z"), stamp); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "proxy_session", Value: token}, digest("csrf:" + token)
}

func (p *reissuePanel) reissue(t *testing.T, id, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", "/api/v1/nodes/"+id+"/enrollment-token", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	p.mux.ServeHTTP(response, request)
	return response
}

// A node that is still pending can be given a new registration code, for example
// after the first one expired. The old code must not keep working: a re-issue
// replaces it instead of adding a second live code.
func TestReissueEnrollmentReplacesPendingToken(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n, previous, _, err := p.repository.CreateEnrollment(ctx, "Pending", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := p.session(t, "Owner")
	response := p.reissue(t, n.ID, `{"expiresInSeconds":900}`, cookie, csrf)
	if response.Code != 201 {
		t.Fatalf("reissue: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
		EnrollmentToken string `json:"enrollmentToken"`
		ExpiresAt       string `json:"expiresAt"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.EnrollmentToken == "" || body.EnrollmentToken == previous || body.Node.ID != n.ID || body.ExpiresAt == "" {
		t.Fatalf("reissued enrollment: %+v", body)
	}
	if _, err := p.repository.EnrollmentNode(ctx, Hash(previous)); !errors.Is(err, ErrToken) {
		t.Fatalf("the previous enrollment token still enrolls: %v", err)
	}
	resolved, err := p.repository.EnrollmentNode(ctx, Hash(body.EnrollmentToken))
	if err != nil || resolved != n.ID {
		t.Fatalf("the new enrollment token resolves to %q, want %q: %v", resolved, n.ID, err)
	}
	if rows := auditRows(t, p.repository, ctx, "重新签发节点注册码"); rows != 1 {
		t.Fatalf("audit rows: %d", rows)
	}
}

// An Agent has already used the code and holds an identity: re-issuing would let
// a second machine enroll as the same node, so the panel refuses.
func TestReissueEnrollmentRefusesEnrolledNode(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n := enrolledNode(t, p.repository, ctx, "Enrolled")
	cookie, csrf := p.session(t, "Owner")
	response := p.reissue(t, n.ID, `{}`, cookie, csrf)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "node_not_pending") {
		t.Fatalf("enrolled node reissued: %d %s", response.Code, response.Body.String())
	}
	var tokens int
	if err := p.database.QueryRowContext(ctx, "SELECT count(*) FROM node_enrollment_tokens WHERE node_id=? AND consumed_at IS NULL", n.ID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 1 {
		t.Fatalf("the refused reissue changed the enrollment tokens: %d left", tokens)
	}
}

func TestReissueEnrollmentRefusesDeletedNode(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n, _, _, err := p.repository.CreateEnrollment(ctx, "Deleted", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.repository.Delete(ctx, n.ID, nil, nil, "owner", "192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := p.session(t, "Owner")
	response := p.reissue(t, n.ID, `{}`, cookie, csrf)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "node_not_pending") {
		t.Fatalf("deleted node reissued: %d %s", response.Code, response.Body.String())
	}
}

func TestReissueEnrollmentUnknownNode(t *testing.T) {
	p := newReissuePanel(t)
	cookie, csrf := p.session(t, "Owner")
	if response := p.reissue(t, "missing-node", `{}`, cookie, csrf); response.Code != 404 {
		t.Fatalf("unknown node reissued: %d %s", response.Code, response.Body.String())
	}
}

// The expiry bounds are checked before the node is touched, so a code that is
// too short-lived never invalidates the code the Owner already holds.
func TestReissueEnrollmentRejectsShortExpiry(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n, previous, _, err := p.repository.CreateEnrollment(ctx, "Pending", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := p.session(t, "Owner")
	response := p.reissue(t, n.ID, `{"expiresInSeconds":30}`, cookie, csrf)
	if response.Code != 400 {
		t.Fatalf("short expiry accepted: %d %s", response.Code, response.Body.String())
	}
	if _, err := p.repository.EnrollmentNode(ctx, Hash(previous)); err != nil {
		t.Fatalf("a rejected reissue invalidated the pending token: %v", err)
	}
}

func TestReissueEnrollmentRequiresOwner(t *testing.T) {
	p := newReissuePanel(t)
	ctx := context.Background()
	n, previous, _, err := p.repository.CreateEnrollment(ctx, "Pending", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := p.session(t, "Viewer")
	response := p.reissue(t, n.ID, `{}`, cookie, csrf)
	if response.Code != 403 {
		t.Fatalf("viewer reissued an enrollment token: %d %s", response.Code, response.Body.String())
	}
	if _, err := p.repository.EnrollmentNode(ctx, Hash(previous)); err != nil {
		t.Fatalf("a refused reissue invalidated the pending token: %v", err)
	}
}
