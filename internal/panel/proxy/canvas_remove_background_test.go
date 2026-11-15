package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"proxycontrol/internal/panel/auth"
	"strings"
	"testing"
	"time"
)

func TestCanvasRemovalHandlerBoundedWaitAndAudit(t *testing.T) {
	p, scheme, _, deploy := removeFixture(t, "create")
	entered, release := make(chan struct{}), make(chan struct{})
	deploy.afterPublish = func(phase int) {
		if phase == 1 {
			close(entered)
			<-release
		}
	}
	audit := &canvasAudit{}
	h := &Handler{Service: p.service, Users: p.users, Audit: audit}
	h.Auth = &auth.Handler{Service: &auth.Service{Repository: &auth.Repository{Database: p.database}, Settings: canvasSettings{}, SessionDuration: time.Hour}}
	mux := http.NewServeMux()
	h.Register(mux)
	owner, csrf := p.session("Owner")
	viewer, _ := p.session("Viewer")
	for _, cookie := range []*http.Cookie{nil, viewer} {
		r := httptest.NewRequest("GET", "/api/v1/canvas-schemes/"+scheme.ID+"/remove-status", nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("remove-status exposed to non-owner", w.Code)
		}
	}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/canvas-schemes/"+scheme.ID+"/remove", strings.NewReader(`{"updatedAt":"`+scheme.UpdatedAt+`","acknowledgeChanges":true}`))
		r.AddCookie(owner)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	start := time.Now()
	w := request()
	if elapsed := time.Since(start); elapsed > 10*time.Second || w.Code != 202 || !strings.Contains(w.Body.String(), `"status":"removing"`) {
		t.Fatalf("response %s: %d %s", elapsed, w.Code, w.Body.String())
	}
	<-entered
	duplicate := request()
	if duplicate.Code != 202 {
		t.Fatal(duplicate.Code, duplicate.Body.String())
	}
	if len(audit.actions) != 0 {
		t.Fatal("audit written before completion")
	}
	statusRequest := httptest.NewRequest("GET", "/remove-status", nil)
	statusRequest.SetPathValue("id", scheme.ID)
	statusResponse := httptest.NewRecorder()
	h.canvasRemovalStatus(statusResponse, statusRequest)
	if !strings.Contains(statusResponse.Body.String(), `"state":"removing"`) {
		t.Fatal(statusResponse.Body.String())
	}
	p.service.removalMu.Lock()
	run := p.service.removalRuns[scheme.ID]
	p.service.removalMu.Unlock()
	close(release)
	<-run.done
	if run.err != nil || len(audit.actions) != 1 || len(deploy.phases) != 2 {
		t.Fatalf("result: %v audit=%v phases=%d", run.err, audit.actions, len(deploy.phases))
	}
}

type shutdownRemovalDeployer struct {
	*removalDeployer
	entered  chan struct{}
	canceled int
}

func (d *shutdownRemovalDeployer) PublishCanvasRemoval(ctx context.Context, name, author string, snapshots []NodeSnapshot) (CanvasRemovePublication, error) {
	result, err := d.removalDeployer.PublishCanvasRemoval(ctx, name, author, snapshots)
	if err != nil {
		return result, err
	}
	close(d.entered)
	<-ctx.Done()
	return result, ctx.Err()
}
func (d *shutdownRemovalDeployer) CancelCanvasRemoval(context.Context, CanvasRemovePublication) error {
	d.canceled++
	return nil
}

func TestCanvasRemovalPanelShutdownCancelsAndWaits(t *testing.T) {
	p, scheme, binding, base := removeFixture(t, "create")
	deploy := &shutdownRemovalDeployer{removalDeployer: base, entered: make(chan struct{})}
	p.service.Deploy = deploy
	run, _, err := p.service.startCanvasRemoval(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-deploy.entered
	p.service.StopCanvasRemovals()
	<-run.done
	if deploy.canceled != 1 || base.rollbacks != 0 || p.service.CanvasRemovalStatus(scheme.ID).State != "failed" {
		t.Fatal("shutdown did not cancel exactly once")
	}
	inbound, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false)
	if err != nil || !inbound.Enabled {
		t.Fatal("local intent not restored", err)
	}
	if _, _, err := p.service.startCanvasRemoval(p.ctx, scheme.ID, CanvasRemoveInput{}, "owner", p.users, nil); err == nil {
		t.Fatal("shutdown accepted new work")
	}
}

type shutdownRollbackDeployer struct{ *shutdownRemovalDeployer }

func (d *shutdownRollbackDeployer) PublishCanvasRemoval(ctx context.Context, name, author string, snapshots []NodeSnapshot) (CanvasRemovePublication, error) {
	result, err := d.removalDeployer.PublishCanvasRemoval(ctx, name, author, snapshots)
	if err != nil {
		return result, err
	}
	return result, errors.New("controlled apply failure")
}
func (d *shutdownRollbackDeployer) RollbackCanvasRemoval(ctx context.Context, _ CanvasRemovePublication) error {
	close(d.entered)
	<-ctx.Done()
	return ctx.Err()
}
func TestCanvasRemovalShutdownInterruptsFailureRollback(t *testing.T) {
	p, scheme, _, base := removeFixture(t, "create")
	d := &shutdownRollbackDeployer{&shutdownRemovalDeployer{removalDeployer: base, entered: make(chan struct{})}}
	p.service.Deploy = d
	run, _, err := p.service.startCanvasRemoval(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-d.entered
	stopped := make(chan struct{})
	go func() { p.service.StopCanvasRemovals(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown blocked on rollback receipt")
	}
	<-run.done
	if d.canceled != 1 {
		t.Fatal("rollback not handed to persistent recovery", d.canceled)
	}
}

func TestCanvasRemovalDetachedDuplicateAndStatus(t *testing.T) {
	p, scheme, _, deploy := removeFixture(t, "create")
	entered, release := make(chan struct{}), make(chan struct{})
	deploy.afterPublish = func(phase int) {
		if phase == 1 {
			close(entered)
			<-release
		}
	}
	ctx, cancel := context.WithCancel(p.ctx)
	run, duplicate, err := p.service.startCanvasRemoval(ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users, nil)
	if err != nil || duplicate {
		t.Fatalf("start: %v %v", duplicate, err)
	}
	<-entered
	cancel()
	if got := p.service.CanvasRemovalStatus(scheme.ID); got.State != "removing" {
		t.Fatal(got)
	}
	again, duplicate, err := p.service.startCanvasRemoval(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: "stale"}, "owner", p.users, nil)
	if err != nil || !duplicate || again != run {
		t.Fatalf("duplicate: %v %v", duplicate, err)
	}
	close(release)
	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
		t.Fatal("removal did not finish")
	}
	got := p.service.CanvasRemovalStatus(scheme.ID)
	if got.State != "succeeded" || got.Result == nil || !got.Result.Removed || deploy.rollbacks != 0 || len(deploy.phases) != 2 {
		t.Fatalf("outcome: %+v rollback=%d phases=%d", got, deploy.rollbacks, len(deploy.phases))
	}
}

func TestCanvasRemovalFailedStatus(t *testing.T) {
	p, scheme, _, deploy := removeFixture(t, "create")
	entered, release := make(chan struct{}), make(chan struct{})
	deploy.failAt = 1
	deploy.afterPublish = func(int) { close(entered); <-release }
	run, _, err := p.service.startCanvasRemoval(p.ctx, scheme.ID, CanvasRemoveInput{UpdatedAt: scheme.UpdatedAt, AcknowledgeChanges: true}, "owner", p.users, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if got := p.service.CanvasRemovalStatus(scheme.ID); got.State != "removing" {
		t.Fatal(got)
	}
	close(release)
	<-run.done
	got := p.service.CanvasRemovalStatus(scheme.ID)
	if got.State != "failed" || got.Error == nil || deploy.rollbacks != 1 {
		t.Fatalf("%+v", got)
	}
	if p.service.CanvasRemovalStatus("missing").State != "idle" {
		t.Fatal("missing outcome is not idle")
	}
}
