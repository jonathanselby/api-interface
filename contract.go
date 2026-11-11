package apiinterface

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Fault is a deliberately small classification, not a wrapper for internal details.
type Fault string

const (
	Invalid          Fault = "invalid_request"
	Unauthenticated  Fault = "unauthenticated"
	Forbidden        Fault = "forbidden"
	NotFound         Fault = "not_found"
	Conflict         Fault = "conflict"
	Unprocessable    Fault = "unprocessable"
	Overloaded       Fault = "overloaded"
	TooLarge         Fault = "too_large"
	UnsupportedMedia Fault = "unsupported_media_type"
	Unavailable      Fault = "unavailable"
	Internal         Fault = "internal"
)

type APIError struct{ Fault Fault }

func (e *APIError) Error() string { return string(e.Fault) }
func Problem(f Fault) error       { return &APIError{Fault: f} }

var faultMap = map[Fault]struct {
	status  int
	message string
}{
	Invalid: {400, "invalid request"}, Unauthenticated: {401, "authentication required"},
	Forbidden: {403, "forbidden"}, NotFound: {404, "not found"},
	Conflict: {409, "request conflicts with current state"}, Unprocessable: {422, "invalid operation"},
	Overloaded: {429, "temporarily overloaded"}, TooLarge: {413, "request body too large"},
	UnsupportedMedia: {415, "unsupported media type"}, Unavailable: {503, "service unavailable"},
	Internal: {500, "internal server error"},
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var typed *APIError
	fault := Internal
	if errors.As(err, &typed) {
		fault = typed.Fault
	}
	d, ok := faultMap[fault]
	if !ok {
		d = faultMap[Internal]
		fault = Internal
	}
	requestID := ""
	if rc, ok := RequestContextFrom(r.Context()); ok {
		requestID = rc.RequestID
	}
	WriteJSON(w, d.status, struct {
		Error struct {
			Code      Fault  `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}{
		Error: struct {
			Code      Fault  `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		}{fault, d.message, requestID},
	})
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "internal server error", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// DecodeJSON bounds the entire document, checks UTF-8 and enforces one JSON value.
// Command inputs reject unknown fields by default.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		return Problem(Internal)
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return Problem(UnsupportedMedia)
	}
	bounded := http.MaxBytesReader(w, r.Body, maxBytes)
	defer bounded.Close()
	raw, err := io.ReadAll(bounded)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return Problem(TooLarge)
		}
		return Problem(Invalid)
	}
	if !utf8.Valid(raw) {
		return Problem(Invalid)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return Problem(Invalid)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Problem(Invalid)
	}
	return nil
}

type Page struct {
	Limit  int
	Cursor string
}

type Pagination struct{ DefaultLimit, MaxLimit, MaxCursorBytes int }

func (p Pagination) Parse(r *http.Request) (Page, error) {
	if p.DefaultLimit <= 0 || p.MaxLimit < p.DefaultLimit || p.MaxCursorBytes <= 0 {
		return Page{}, Problem(Internal)
	}
	query := r.URL.Query()
	if len(query["limit"]) > 1 || len(query["cursor"]) > 1 {
		return Page{}, Problem(Invalid)
	}
	result := Page{Limit: p.DefaultLimit}
	if s := query.Get("limit"); s != "" {
		for _, c := range s {
			if c < '0' || c > '9' {
				return Page{}, Problem(Invalid)
			}
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > p.MaxLimit {
			return Page{}, Problem(Invalid)
		}
		result.Limit = n
	}
	result.Cursor = query.Get("cursor")
	if len(result.Cursor) > p.MaxCursorBytes || !utf8.ValidString(result.Cursor) {
		return Page{}, Problem(Invalid)
	}
	return result, nil
}

func IdempotencyKey(r *http.Request) (string, error) {
	vals := r.Header.Values("Idempotency-Key")
	if len(vals) == 0 {
		return "", nil
	}
	if len(vals) != 1 || !requestToken(vals[0], 128) {
		return "", Problem(Invalid)
	}
	return vals[0], nil
}

func validRouteMethod(s string) bool {
	switch strings.ToUpper(s) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return s == strings.ToUpper(s)
	}
	return false
}
