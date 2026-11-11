package apiinterface

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func oneRoute() Route {
	return Route{Method: "POST", Path: "/api/v1/test/{id}", OperationID: "test", Summary: "Test operation",
		Parameters: []Parameter{{Name: "id", In: "path", Required: true, Schema: json.RawMessage(`{"type":"string"}`)}},
		Request:    &JSONBody{Required: true, Schema: json.RawMessage(`{"type":"object"}`)},
		Responses:  []Response{{Status: 200, Description: "Success", Schema: json.RawMessage(`{"type":"object"}`)}},
		Handler:    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { WriteJSON(w, 200, map[string]any{"ok": true}) })}
}

func testAPI(t *testing.T, routes ...Route) *API {
	t.Helper()
	a, err := New(Config{Title: "Test", Version: Version, MaxBodyBytes: 1024}, routes)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestOpenAPIValidation(t *testing.T) {
	route := oneRoute()
	a := testAPI(t, route)
	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest("GET", "/openapi.json", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	var spec struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.0.3" || spec.Info.Title != "Test" || spec.Info.Version != Version {
		t.Fatalf("incorrect OpenAPI header: %+v", spec)
	}
	if len(spec.Paths) != 1 || len(spec.Paths[route.Path]) != 1 || len(spec.Paths[route.Path]["post"]) == 0 {
		t.Fatalf("missing or extra route: %+v", spec.Paths)
	}
	var op struct {
		OperationID string                     `json:"operationId"`
		Parameters  []Parameter                `json:"parameters"`
		Responses   map[string]json.RawMessage `json:"responses"`
		RequestBody json.RawMessage            `json:"requestBody"`
	}
	if err := json.Unmarshal(spec.Paths[route.Path]["post"], &op); err != nil {
		t.Fatal(err)
	}
	if op.OperationID != "test" || len(op.Parameters) != 1 || len(op.Responses) != 1 || len(op.RequestBody) == 0 {
		t.Fatalf("incomplete operation: %+v", op)
	}
	if !reflect.DeepEqual(a.OpenAPI(), a.OpenAPI()) {
		t.Fatal("nondeterministic OpenAPI")
	}
	copied := a.OpenAPI()
	copied[0] = '!'
	if !json.Valid(a.OpenAPI()) {
		t.Fatal("OpenAPI mutable by caller")
	}
}

func TestDuplicateAndIncompleteRegistrations(t *testing.T) {
	r := oneRoute()
	for _, routes := range [][]Route{{r, r}, {{Method: "GET", Path: "/api/v1/x", OperationID: "x", Summary: "Incomplete"}}} {
		if _, err := New(Config{Title: "Test", Version: "0.1.0", MaxBodyBytes: 100}, routes); err == nil {
			t.Fatal("accepted bad registration")
		}
	}
	r.Parameters = nil
	if _, err := New(Config{Title: "Test", Version: "0.1.0", MaxBodyBytes: 100}, []Route{r}); err == nil {
		t.Fatal("undocumented path variable accepted")
	}
}

func TestErrorContract(t *testing.T) {
	tests := []struct {
		fault  Fault
		status int
	}{{Invalid, 400}, {Unauthenticated, 401}, {Forbidden, 403}, {NotFound, 404}, {Conflict, 409}, {Unprocessable, 422}, {Overloaded, 429}, {TooLarge, 413}, {UnsupportedMedia, 415}, {Internal, 500}, {Unavailable, 503}}
	for _, tt := range tests {
		t.Run(string(tt.fault), func(t *testing.T) {
			a := testAPI(t, Route{Method: "GET", Path: "/api/v1/error", OperationID: "failure", Summary: "Return an error", Responses: []Response{{Status: tt.status, Description: "Error"}}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { WriteError(w, r, Problem(tt.fault)) })})
			rr := httptest.NewRecorder()
			a.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/error", nil))
			if rr.Code != tt.status {
				t.Fatalf("status %d != %d", rr.Code, tt.status)
			}
			var body struct {
				Error struct {
					Code      string `json:"code"`
					Message   string `json:"message"`
					RequestID string `json:"request_id"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != string(tt.fault) || body.Error.RequestID == "" || body.Error.Message == "" {
				t.Fatalf("bad envelope: %+v", body)
			}
			if rr.Header().Get("X-Request-ID") != body.Error.RequestID {
				t.Fatal("request ID mismatch")
			}
		})
	}
	secret := "postgres://secret:password@db"
	rr := httptest.NewRecorder()
	WriteError(rr, httptest.NewRequest("GET", "/", nil), errors.New(secret))
	if strings.Contains(rr.Body.String(), secret) || rr.Code != 500 {
		t.Fatal("leaked internal detail")
	}
}

func TestDecodeStrictJSONAndLimits(t *testing.T) {
	test := func(body, contentType string, expected int) {
		t.Helper()
		a := testAPI(t, Route{Method: "POST", Path: "/api/v1/json", OperationID: "json", Summary: "JSON", Responses: []Response{{Status: 200, Description: "OK"}}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Name string `json:"name"`
			}
			if err := DecodeJSON(w, r, &req, 16); err != nil {
				WriteError(w, r, err)
				return
			}
			WriteJSON(w, 200, map[string]string{"name": req.Name})
		})})
		req := httptest.NewRequest("POST", "/api/v1/json", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		rr := httptest.NewRecorder()
		a.ServeHTTP(rr, req)
		if rr.Code != expected {
			t.Fatalf("body %q got %d expected %d: %s", body, rr.Code, expected, rr.Body.String())
		}
	}
	test(`{"name":"x"}`, "application/json", 200) // valid bounded JSON
	test(`{"x":0}`, "application/json", 400)
	test(`{"name":`, "application/json", 400)
	test(`{}`, "text/plain", 415)
	test(`{}`, "application/json", 200)
	test(`{} {}`, "application/json", 400)
	test(string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), "application/json", 400)
	test(strings.Repeat("x", 2000), "application/json", 413) // API-level bound
}

func TestPaginationContract(t *testing.T) {
	p := Pagination{DefaultLimit: 20, MaxLimit: 100, MaxCursorBytes: 4}
	checks := []struct {
		query string
		ok    bool
		limit int
	}{
		{"", true, 20}, {"limit=3&cursor=abc", true, 3}, {"limit=101", false, 0},
		{"limit=-1", false, 0}, {"limit=2&limit=3", false, 0}, {"cursor=abcde", false, 0},
		{"cursor=x&cursor=y", false, 0}, {"limit=1e3", false, 0},
	}
	for _, c := range checks {
		req := httptest.NewRequest("GET", "/api/v1/items?"+c.query, nil)
		page, err := p.Parse(req)
		if (err == nil) != c.ok || (c.ok && page.Limit != c.limit) {
			t.Fatalf("query %q returned %+v %v", c.query, page, err)
		}
	}
}

func TestIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Idempotency-Key", "safe-key_123")
	if key, err := IdempotencyKey(req); err != nil || key != "safe-key_123" {
		t.Fatalf("%q %v", key, err)
	}
	req.Header.Add("Idempotency-Key", "second")
	if _, err := IdempotencyKey(req); err == nil {
		t.Fatal("duplicate idempotency header accepted")
	}
	req.Header.Set("Idempotency-Key", strings.Repeat("x", 129))
	if _, err := IdempotencyKey(req); err == nil {
		t.Fatal("oversized key accepted")
	}
}

func TestRequestContextPropagation(t *testing.T) {
	var seen RequestContext
	a := testAPI(t, Route{Method: "GET", Path: "/api/v1/context", OperationID: "context", Summary: "Context", Responses: []Response{{Status: 200, Description: "OK"}}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		seen, ok = RequestContextFrom(r.Context())
		if !ok {
			t.Error("no context")
		}
		WriteJSON(w, 200, map[string]any{"ok": true})
	})})
	req := httptest.NewRequest("GET", "/api/v1/context", nil)
	req.Header.Set("X-Correlation-ID", "correlation:123")
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("X-Identity-ID", "forged")
	req.Header.Set("X-Client-ID", "forged")
	req.Header.Set("X-Forwarded-For", "203.0.113.88")
	req = req.WithContext(WithTrustedIdentity(req.Context(), Identity{IdentityID: "confirmed-user", ClientID: "confirmed-client"}))
	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, req)
	if rr.Code != 200 || seen.IdentityID != "confirmed-user" || seen.ClientID != "confirmed-client" || seen.CorrelationID != "correlation:123" || seen.TraceParent == "" || seen.RequestID == "" || seen.ReceivedAt.IsZero() || seen.ClientIP == "203.0.113.88" {
		t.Fatalf("context not preserved: %+v", seen)
	}
	if time.Since(seen.ReceivedAt) > time.Minute {
		t.Fatal("bad time")
	}
	req2 := httptest.NewRequest("GET", "/api/v1/context", nil)
	req2.Header.Set("X-Identity-ID", "forged")
	rr = httptest.NewRecorder()
	a.ServeHTTP(rr, req2)
	if seen.IdentityID != "" || seen.ClientID != "" {
		t.Fatalf("untrusted headers established identity: %+v", seen)
	}
	req2.Header.Set("Traceparent", "00-invalid")
	rr = httptest.NewRecorder()
	a.ServeHTTP(rr, req2)
	if rr.Code != 400 {
		t.Fatal("invalid trace accepted")
	}
}

func TestTrustedProxyMetadata(t *testing.T) {
	a, err := New(Config{Title: "Test", Version: "0.1.0", MaxBodyBytes: 100, TrustedProxyCIDRs: []string{"127.0.0.1/32"}}, []Route{{Method: "GET", Path: "/api/v1/x", OperationID: "x", Summary: "Context", Responses: []Response{{Status: 200, Description: "OK"}}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc, _ := RequestContextFrom(r.Context())
		WriteJSON(w, 200, rc)
	})}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/x", nil)
	req.RemoteAddr = "127.0.0.1:3210"
	req.Header.Set("X-Forwarded-For", "198.51.100.99, 203.0.113.20")
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, req)
	var info RequestContext
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.ClientIP != "203.0.113.20" || info.Scheme != "https" {
		t.Fatalf("incorrect trusted proxy context: %+v", info)
	}
	req.RemoteAddr = "192.0.2.1:3210"
	rr = httptest.NewRecorder()
	a.ServeHTTP(rr, req)
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.ClientIP != "192.0.2.1" || info.Scheme == "https" {
		t.Fatalf("untrusted forwarding metadata accepted: %+v", info)
	}
}

func TestDependencyBoundary(t *testing.T) {
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "require") {
		t.Fatal("runtime dependencies entered component")
	}
}

func TestVersionMatchesFile(t *testing.T) {
	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != Version {
		t.Fatalf("version %q != %q", Version, strings.TrimSpace(string(raw)))
	}
}

func TestClientDisconnectPropagatesContext(t *testing.T) {
	cancelled := make(chan struct{})
	a := testAPI(t, Route{Method: "GET", Path: "/api/v1/wait", OperationID: "wait", Summary: "Wait", Responses: []Response{{Status: 200, Description: "OK"}}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done(); close(cancelled) })})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	// Ensure the request reaches the handler before cancellation.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); a.ServeHTTP(w, r) })
	server := httptest.NewServer(handler)
	defer server.Close()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/wait", nil)
	done := make(chan struct{})
	go func() { _, _ = http.DefaultClient.Do(req); close(done) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request never started")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("application did not observe cancellation")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client blocked")
	}
}

func TestProxyCIDRRejectsInvalid(t *testing.T) {
	if _, err := New(Config{Title: "Test", Version: "0.1.0", MaxBodyBytes: 100, TrustedProxyCIDRs: []string{"nope"}}, nil); err == nil {
		t.Fatal("bad proxy CIDR accepted")
	}
}

// The test uses an actual TCP address to guard against tests accidentally
// supplying a forged Forwarded-For value as the immediate peer.
func TestParseRemoteAddr(t *testing.T) {
	a := testAPI(t)
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = net.JoinHostPort("198.51.100.1", "90")
	rc, err := a.requestContext(req)
	if err != nil || rc.ClientIP != "198.51.100.1" {
		t.Fatalf("%+v %v", rc, err)
	}
}
