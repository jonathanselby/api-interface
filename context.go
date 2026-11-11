package apiinterface

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// RequestContext contains transport metadata; it is not an authorisation decision.
type RequestContext struct {
	RequestID     string
	CorrelationID string
	IdentityID    string
	ClientID      string
	ReceivedAt    time.Time
	TraceParent   string
	ClientIP      string
	Scheme        string
	Host          string
}

// Identity must be supplied by previously authenticated, trusted server-side middleware.
// Never populate it directly from request headers or forwarded metadata.
type Identity struct{ IdentityID, ClientID string }

type identityKey struct{}
type requestContextKey struct{}

func WithTrustedIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

func RequestContextFrom(ctx context.Context) (RequestContext, bool) {
	v, ok := ctx.Value(requestContextKey{}).(RequestContext)
	return v, ok
}

var tokenRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
var traceRE = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

func requestToken(value string, max int) bool {
	return len(value) > 0 && len(value) <= max && tokenRE.MatchString(value)
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func parseTraceparent(s string) bool {
	if !traceRE.MatchString(s) || strings.HasPrefix(s, "ff-") {
		return false
	}
	parts := strings.Split(s, "-")
	return parts[1] != strings.Repeat("0", 32) && parts[2] != strings.Repeat("0", 16)
}

func (a *API) requestContext(r *http.Request) (RequestContext, error) {
	id := newRequestID()
	rc := RequestContext{RequestID: id, CorrelationID: id, ReceivedAt: time.Now().UTC(),
		Scheme: "http", Host: r.Host}
	if r.TLS != nil {
		rc.Scheme = "https"
	}
	if v := r.Header.Get("X-Correlation-ID"); v != "" {
		if len(r.Header.Values("X-Correlation-ID")) != 1 || !requestToken(v, 128) {
			return rc, errors.New("invalid correlation ID")
		}
		rc.CorrelationID = v
	}
	if v := r.Header.Get("Traceparent"); v != "" {
		if len(r.Header.Values("Traceparent")) != 1 || !parseTraceparent(v) {
			return rc, errors.New("invalid traceparent")
		}
		rc.TraceParent = v
	}
	if identity, ok := r.Context().Value(identityKey{}).(Identity); ok {
		rc.IdentityID, rc.ClientID = identity.IdentityID, identity.ClientID
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer != nil {
		rc.ClientIP = peer.String()
	}
	if peer == nil || !a.trustedPeer(peer) {
		return rc, nil
	}

	// Walk from the trusted immediate peer backwards through trusted hops.
	// Never use the leftmost client-supplied X-Forwarded-For value by default.
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(forwarded) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(forwarded[i]))
		if ip == nil {
			break
		}
		rc.ClientIP = ip.String()
		if !a.trustedPeer(ip) {
			break
		}
	}
	// Host and protocol are informational only; callers must not use them for authority.
	// The trusted proxy must replace/sanitise these fields, not merely forward client values.
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
		rc.Scheme = p
	}
	if h := r.Header.Get("X-Forwarded-Host"); h != "" && len(h) <= 255 &&
		!strings.ContainsAny(h, ",/\\ \t\r\n@") {
		rc.Host = h
	}
	return rc, nil
}
