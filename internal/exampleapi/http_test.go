package exampleapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/jonathanselby/api-interface"
	"github.com/jonathanselby/api-interface/internal/exampleapp"
)

func setup(t *testing.T, ready func(context.Context) error, proxy []string) (*API, *exampleapp.Store) {
	t.Helper()
	store := exampleapp.NewStore()
	a, err := New(store, ready, proxy)
	if err != nil {
		t.Fatal(err)
	}
	return a, store
}

func perform(h http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestExampleAPI(t *testing.T) {
	a, _ := setup(t, nil, nil)
	r := perform(a.Handler, "POST", "/api/v1/items", `{"title":"first"}`, "")
	if r.Code != 201 || !strings.Contains(r.Body.String(), `"id":1`) {
		t.Fatalf("create %d %s", r.Code, r.Body.String())
	}
	r = perform(a.Handler, "GET", "/api/v1/items", "", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"title":"first"`) {
		t.Fatalf("list %d %s", r.Code, r.Body.String())
	}
	r = perform(a.Handler, "GET", "/health", "", "")
	if r.Code != 200 {
		t.Fatalf("health %d", r.Code)
	}
	r = perform(a.Handler, "GET", "/ready", "", "")
	if r.Code != 200 {
		t.Fatalf("ready %d", r.Code)
	}
	r = perform(a.Handler, "GET", "/openapi.json", "", "")
	if r.Code != 200 {
		t.Fatalf("OpenAPI %d", r.Code)
	}
}

func TestIdempotencyKey(t *testing.T) {
	a, _ := setup(t, nil, nil)
	create := func(title, key string) (int, string) {
		r := perform(a.Handler, "POST", "/api/v1/items", fmt.Sprintf(`{"title":%q}`, title), key)
		return r.Code, r.Body.String()
	}
	s1, b1 := create("alpha", "key-1")
	s2, b2 := create("alpha", "key-1")
	if s1 != 201 || s2 != 201 || b1 != b2 {
		t.Fatalf("retry not idempotent: %d %s %d %s", s1, b1, s2, b2)
	}
	if status, _ := create("other", "key-1"); status != 409 {
		t.Fatal("different payload under same key not refused")
	}
	s3, b3 := create("alpha", "")
	if s3 != 201 || b1 == b3 {
		t.Fatal("key-free operation incorrectly replayed")
	}
	if s4, _ := create("", "new-key"); s4 != 422 {
		t.Fatal("invalid application input was not refused")
	}
}

func TestPaginationContract(t *testing.T) {
	a, _ := setup(t, nil, nil)
	for i := 0; i < 5; i++ {
		r := perform(a.Handler, "POST", "/api/v1/items", fmt.Sprintf(`{"title":"item-%d"}`, i), "")
		if r.Code != 201 {
			t.Fatal(r.Code)
		}
	}
	first := perform(a.Handler, "GET", "/api/v1/items?limit=2", "", "")
	var page exampleapp.Result
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != 1 || page.NextCursor == "" {
		t.Fatalf("bad first page: %+v", page)
	}
	second := perform(a.Handler, "GET", "/api/v1/items?limit=2&cursor="+url.QueryEscape(page.NextCursor), "", "")
	page = exampleapp.Result{}
	if err := json.Unmarshal(second.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != 3 || page.NextCursor == "" {
		t.Fatalf("bad second page: %+v", page)
	}
	third := perform(a.Handler, "GET", "/api/v1/items?limit=2&cursor="+url.QueryEscape(page.NextCursor), "", "")
	page = exampleapp.Result{}
	if err := json.Unmarshal(third.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 5 || page.NextCursor != "" {
		t.Fatalf("bad final page: %+v", page)
	}
	for _, path := range []string{"/api/v1/items?limit=101", "/api/v1/items?cursor=broken!", "/api/v1/items?limit=1&limit=2"} {
		rr := perform(a.Handler, "GET", path, "", "")
		if rr.Code == 200 {
			t.Fatalf("bad pagination accepted: %s", path)
		}
	}
}

func TestReadinessIsolation(t *testing.T) {
	a, _ := setup(t, func(context.Context) error { return errors.New("secret: database down") }, nil)
	if r := perform(a.Handler, "GET", "/health", "", ""); r.Code != 200 {
		t.Fatal("liveness incorrectly coupled to readiness")
	}
	r := perform(a.Handler, "GET", "/ready", "", "")
	if r.Code != 503 || strings.Contains(r.Body.String(), "database") {
		t.Fatalf("readiness leakage: %d %s", r.Code, r.Body.String())
	}
}

func TestRequestContextPropagation(t *testing.T) {
	spy := &contextSpy{}
	a, err := New(spy, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/items", strings.NewReader(`{"title":"alpha"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", "case-1")
	req.Header.Set("X-Identity-ID", "forged")
	req = req.WithContext(api.WithTrustedIdentity(req.Context(), api.Identity{IdentityID: "trusted", ClientID: "client-1"}))
	rr := httptest.NewRecorder()
	a.Handler.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	rc := spy.context()
	if rc.RequestID == "" || rc.CorrelationID != "case-1" || rc.IdentityID != "trusted" || rc.ClientID != "client-1" {
		t.Fatalf("context not received by domain: %+v", rc)
	}
}

type contextSpy struct {
	mu   sync.Mutex
	last api.RequestContext
}

func (s *contextSpy) Create(ctx context.Context, title, key string) (exampleapp.Item, error) {
	rc, _ := api.RequestContextFrom(ctx)
	s.mu.Lock()
	s.last = rc
	s.mu.Unlock()
	return exampleapp.Item{ID: 1, Title: title}, nil
}
func (s *contextSpy) List(ctx context.Context, limit int, cursor string) (exampleapp.Result, error) {
	return exampleapp.Result{}, nil
}
func (s *contextSpy) context() api.RequestContext { s.mu.Lock(); defer s.mu.Unlock(); return s.last }

func TestBehindReverseProxy(t *testing.T) {
	a, _ := setup(t, nil, []string{"127.0.0.1/32"})
	backend := httptest.NewServer(a.Handler)
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	proxy := httputil.NewSingleHostReverseProxy(backendURL)
	edge := httptest.NewServer(proxy)
	defer edge.Close()
	client := http.Client{Timeout: 2 * time.Second}
	for _, base := range []string{backend.URL, edge.URL} {
		req, _ := http.NewRequest("POST", base+"/api/v1/items", strings.NewReader(`{"title":"via-proxy"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Identity-ID", "forged")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 201 {
			t.Fatalf("%s failed: %s", base, raw)
		}
	}
	resp, err := client.Get(edge.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("proxied OpenAPI: %d", resp.StatusCode)
	}
}

func TestRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	s := &blockingService{started: started, cancelled: cancelled}
	a, err := New(s, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/items", bytes.NewReader([]byte(`{"title":"slow"}`)))
	req.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
		}
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("no request")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("domain did not get cancellation")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not terminate")
	}
}

type blockingService struct{ started, cancelled chan struct{} }

func (s *blockingService) Create(ctx context.Context, title, key string) (exampleapp.Item, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	return exampleapp.Item{}, ctx.Err()
}
func (s *blockingService) List(ctx context.Context, limit int, cursor string) (exampleapp.Result, error) {
	return exampleapp.Result{}, nil
}

func TestRequestLimits(t *testing.T) {
	a, _ := setup(t, nil, nil)
	large := perform(a.Handler, "POST", "/api/v1/items", `{"title":"`+strings.Repeat("a", 65536)+`"}`, "")
	if large.Code != 413 {
		t.Fatalf("body bound: %d", large.Code)
	}
	srv := httptest.NewUnstartedServer(a.Handler)
	srv.Config.ReadHeaderTimeout = 80 * time.Millisecond
	srv.Config.ReadTimeout = 500 * time.Millisecond
	srv.Config.WriteTimeout = time.Second
	srv.Config.IdleTimeout = time.Second
	srv.Config.MaxHeaderBytes = 1024
	srv.Start()
	defer srv.Close()
	conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(conn, "GET /health HTTP/1.1\r\nHost: localhost\r\nX-Huge: "+strings.Repeat("A", 16384)+"\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("large header: %v", err)
	}
	resp.Body.Close()
	conn.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("header limit %d", resp.StatusCode)
	}

	slow, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	_, _ = io.WriteString(slow, "GET /health HTTP/1.1\r\nHost: ")
	_ = slow.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, 512)
	_, err = slow.Read(b)
	if err != nil && err != io.EOF {
		t.Fatalf("header timeout did not close or respond: %v", err)
	}
	// Timeout must not let a partial request reach the application's handler.
}

func TestArchitectureBoundary(t *testing.T) {
	// Dependency rule is structural, not a comment in an EDD.
	file := filepath.Join("..", "exampleapp", "app.go")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"net/http"`)) {
		t.Fatal("domain package imports HTTP")
	}
}

func TestGracefulShutdown(t *testing.T) {
	a, _ := setup(t, nil, nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: a.Handler, ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 16 << 10}
	stopped := make(chan error, 1)
	go func() { stopped <- s.Serve(listener) }()
	response, err := http.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != http.ErrServerClosed {
		t.Fatalf("serve did not stop cleanly: %v", err)
	}
}
