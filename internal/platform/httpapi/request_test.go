package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeReportsOversizedBody(t *testing.T) {
	body := `{"value":"` + strings.Repeat("a", 1<<20) + `"}`
	request := httptest.NewRequest("POST", "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	var target struct {
		Value string `json:"value"`
	}
	err := Decode(httptest.NewRecorder(), request, &target)
	if err == nil || err.Error() != "请求内容超过 1 MiB 上限" {
		t.Fatalf("oversized body: %v", err)
	}
	request = httptest.NewRequest("POST", "/", strings.NewReader(`{"value":1}`))
	request.Header.Set("Content-Type", "application/json")
	if err := Decode(httptest.NewRecorder(), request, &target); err == nil || err.Error() != "invalid JSON request" {
		t.Fatalf("bad JSON: %v", err)
	}
}
