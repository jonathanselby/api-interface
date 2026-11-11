package exampleapi

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestReadBodyTimeout exercises http.Server.ReadTimeout against a real TCP
// client that sends headers but never completes the announced request body.
func TestReadBodyTimeout(t *testing.T) {
	readResult := make(chan error, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		readResult <- err
	}))
	srv.Config.ReadHeaderTimeout = time.Second
	srv.Config.ReadTimeout = 150 * time.Millisecond
	srv.Config.WriteTimeout = 2 * time.Second
	srv.Start()
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\n\r\npartial"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readResult:
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("incomplete request body did not time out: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("body read remained blocked past server timeout")
	}
}

// TestWriteTimeout verifies that a stalled handler cannot serve a successful
// response after the configured http.Server.WriteTimeout expires.
func TestWriteTimeout(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(450 * time.Millisecond)
		_, _ = io.WriteString(w, "too late")
	}))
	srv.Config.ReadHeaderTimeout = time.Second
	srv.Config.WriteTimeout = 150 * time.Millisecond
	srv.Start()
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(srv.URL)
	if err != nil {
		return // Connection closed without a successful HTTP response.
	}
	defer res.Body.Close()
	body, readErr := io.ReadAll(res.Body)
	if readErr == nil && res.StatusCode == http.StatusOK && string(body) == "too late" {
		t.Fatal("write timeout allowed response from expired handler")
	}
}

// TestIdleTimeout proves that a connection cannot be reused after the server's
// idle limit, without confusing an ordinary new client connection for reuse.
func TestIdleTimeout(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	srv.Config.ReadHeaderTimeout = time.Second
	srv.Config.IdleTimeout = 150 * time.Millisecond
	srv.Start()
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("initial response: status=%d err=%v", res.StatusCode, err)
	}
	// Give the server ample time beyond its idle deadline, including on CI.
	time.Sleep(450 * time.Millisecond)
	if _, err := io.WriteString(conn, request); err != nil {
		return // Idle connection was closed before the second write.
	}
	if res, err := http.ReadResponse(reader, nil); err == nil {
		res.Body.Close()
		t.Fatal("expired idle connection served another request")
	}
}
