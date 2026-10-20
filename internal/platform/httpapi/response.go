package httpapi

import (
	"encoding/json"
	"net/http"
)

type ErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func Error(w http.ResponseWriter, status int, code, message string) {
	body := ErrorBody{}
	body.Error.Code, body.Error.Message = code, message
	JSON(w, status, body)
}

// ErrorDetails writes the same error body with extra fields inside "error", so a
// client can read machine-readable context such as the blockers of a refused
// deletion.
func ErrorDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	inner := map[string]any{"code": code, "message": message}
	for key, value := range details {
		inner[key] = value
	}
	JSON(w, status, map[string]any{"error": inner})
}
