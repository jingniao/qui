package user

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFailedDeleteDoesNotRevokeAccess is the handler-level guard for deleting a
// user or a device: a delete that did not happen must not schedule a
// revocation, because the credential is still live. The successful path — with
// the ids, usernames and reasons the platform hook receives — is covered
// end-to-end by the automatic revision tests in internal/platform/server.
func TestFailedDeleteDoesNotRevokeAccess(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	removals := []string{}
	handler := &Handler{Repository: repository, AccessRemoved: func(_ context.Context, userID, username, reason string) {
		removals = append(removals, userID+" "+username+" "+reason)
	}}
	remove := func(call func(http.ResponseWriter, *http.Request), id string) int {
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+id, nil)
		request.SetPathValue("id", id)
		recorder := httptest.NewRecorder()
		call(recorder, request)
		return recorder.Code
	}

	// Keep a real user and device to check that failed deletes leave them intact.
	account, err := repository.Save(ctx, "", userInput())
	if err != nil {
		t.Fatal(err)
	}
	device, err := repository.CreateDevice(ctx, account.ID, DeviceInput{Name: "laptop", Protocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	if code := remove(handler.delete, "missing"); code != 404 {
		t.Fatalf("deleting an unknown user: %d", code)
	}
	// The removed device API has no registered route and touches nothing.
	mux := http.NewServeMux()
	handler.Register(mux, handler)
	if code := remove(mux.ServeHTTP, device.ID); code != 404 {
		t.Fatalf("deleting a device: %d", code)
	}
	if len(removals) != 0 {
		t.Fatalf("a failed delete revoked access: %v", removals)
	}
	if _, err := repository.Get(ctx, account.ID); err != nil {
		t.Fatalf("the failed deletes changed the user: %v", err)
	}
	if _, err := repository.Device(ctx, device.ID); err != nil {
		t.Fatalf("the failed deletes changed the device: %v", err)
	}
}
