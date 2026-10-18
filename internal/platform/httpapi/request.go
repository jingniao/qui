package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
)

func Decode(w http.ResponseWriter, r *http.Request, target any) error {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON request")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func Pagination(r *http.Request) (page, size int, err error) {
	page, size = 1, 20
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil {
			return
		}
	}
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		size, err = strconv.Atoi(raw)
		if err != nil {
			return
		}
	}
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		err = errors.New("invalid pagination")
	}
	return
}
