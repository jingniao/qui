package readtiming

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareUsesPatternAndNeverLogsRequestData(t *testing.T) {
	var output bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		done := Start(r.Context(), "sql")
		done()
		w.WriteHeader(http.StatusAccepted)
	})
	request := httptest.NewRequest("GET", "/api/v1/items/secret-id?token=secret-token", nil)
	request.Header.Set("Authorization", "secret-auth")
	response := httptest.NewRecorder()
	Middleware(mux, slog.New(slog.NewJSONHandler(&output, nil))).ServeHTTP(response, request)
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["route"] != "GET /api/v1/items/{id}" || entry["status"] != float64(202) || entry["request_id"] == "" {
		t.Fatalf("incorrect timing: %s", output.String())
	}
	if strings.Contains(output.String(), "secret-") {
		t.Fatalf("leaked request data: %s", output.String())
	}
	if entry["stages"] == nil {
		t.Fatal("missing stages")
	}
}
