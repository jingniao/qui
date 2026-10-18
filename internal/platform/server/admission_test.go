package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStopAdmissionWaitsForAdmittedHandlerAndRejectsNewMutation(t *testing.T) {
	var gate admission
	entered := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan struct{})
	handler := gate.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-finish; w.WriteHeader(204) }))
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/mutation", nil))
		close(done)
	}()
	<-entered
	gate.stop()
	waited := make(chan struct{})
	go func() { gate.wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("snapshot boundary preceded admitted write completion")
	case <-time.After(20 * time.Millisecond):
	}
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, httptest.NewRequest("POST", "/later-mutation", nil))
	if rejected.Code != 503 {
		t.Fatal("new mutation admitted during shutdown", rejected.Code)
	}
	close(finish)
	<-done
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("request drain did not finish")
	}
}
