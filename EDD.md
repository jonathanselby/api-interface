# API Interface — Engineering Design Document

**Version:** 0.1 — implementation baseline  
**PRD:** v0.1, unchanged and controlling

> **We build the API. The customer may supply the gateway.**

## 1. Purpose and ownership

A reusable Go HTTP/JSON boundary for direct or gateway-backed deployments. It owns HTTP translation, context, errors and OpenAPI. Applications own commands, queries, validation, permissions, cursor ordering, idempotency state and readiness checks. Deployments own gateway policy and server configuration.

No gateway, IAM/OAuth server, WAF, service mesh, developer portal, generic CRUD, integration bus, business rules, rate-limit platform or transformation engine.

## 2. Execution model

```text
client -> [optional proxy] -> trusted identity middleware
       -> API Interface (net/http) -> thin handler
       -> application command/query -> JSON response
```

Use `http.ServeMux` and versioned routes (`/api/v1/...`). Expose `http.Handler`; no new router or framework. Domain packages never import `net/http`. The example uses `http.Server`, direct TLS or proxy TLS termination, and graceful `Shutdown`.

## 3. Routes and OpenAPI

Register each endpoint **once**, with handler and operation metadata (method, path, ID, parameters, JSON schemas, statuses). Assemble `/openapi.json` from the registry. No schema reflection or general OpenAPI toolkit. Reject duplicate/incomplete registration; CI verifies route parity and runs an independent OpenAPI 3.0 validator against the generated example document.

## 4. Request context and identity

```go
type RequestContext struct {
    RequestID, CorrelationID, IdentityID, ClientID string
    ReceivedAt time.Time
    TraceParent string
}
```

Mint `RequestID`; bound/validate correlation and trace values. Trusted authentication middleware injects `IdentityID`/`ClientID` into typed context, never from unverified headers. API Interface grants no permissions. Preserve request cancellation; no global request state.

## 5. JSON and errors

Use `encoding/json`, UTF-8, typed responses and `http.MaxBytesReader`. Reject malformed/trailing JSON, oversize bodies and unsupported content types; default to strict command fields.

All mapped failures use `{"error":{"code":"...","message":"...","request_id":"..."}}`. Classify typed errors, not error strings:

- 400 malformed; 401 unauthenticated; 403 unauthorised; 404 missing; 409 conflict; 422 business-invalid; 429 overload if applicable; 500 internal; 503 unavailable.
- 413 oversized input; 415 unsupported media type. Keep internal details out of responses.

Document cancellation/deadline mapping when a response remains possible.

## 6. Idempotency and pagination

Pass bounded `Idempotency-Key` to retry-safe mutations. Application owns replay and persistence.

Parse bounded page size and opaque cursor. Application owns ordering, filters, cursor validity and next position.

## 7. Proxy trust

One API works direct or proxied. Ignore forwarded metadata unless enabled for explicit trusted proxy CIDRs. Never derive identity/permission from forwarding headers. Use relative URLs; no vendor adapter.

## 8. Budgets, health and shutdown

Bound bodies at both API and endpoint level, and bound request metadata. The consuming `http.Server` sets finite header, read/write/idle timeouts and shutdown grace. The example starts on loopback by default and can serve direct TLS or sit behind customer TLS termination. Live-server tests prove enforcement. Propagate cancellation; no internal queues/retries.

`/health` reports liveness; `/ready` calls an application check (200/503). No customer data.

## 9. Failure ownership

Refuse bad input before dispatch. Ignore spoofed identity/proxy headers. Return opaque dependency errors. Liveness may survive unready dependencies. Report incomplete shutdown.

## 10. Dependencies and build

Go 1.26, standard library; test-only OpenAPI validator permitted. Native and `linux/arm64` builds.

## 11. Acceptance-to-evidence map

| Locked PRD criterion | Executable evidence |
|---|---|
| 1. Versioned JSON API | `TestExampleAPI` |
| 2. Thin HTTP handlers | `TestArchitectureBoundary` (domain has no `net/http`) |
| 3. Request context to app | `TestRequestContextPropagation` |
| 4. Stable error envelope/status | `TestErrorContract` |
| 5. Valid matching OpenAPI | `TestOpenAPIValidation` |
| 6. Retry-safe idempotency | `TestIdempotencyKey` (application owns outcome) |
| 7. Bounded deterministic pagination | `TestPaginationContract` |
| 8. Cancellation propagation | `TestRequestCancellation` |
| 9. Body/header/time limits | `TestRequestLimits` (live HTTP server) |
| 10. Direct and reverse proxy | `TestBehindReverseProxy` (including spoofing) |
| 11. No gateway/IAM implementation | `TestDependencyBoundary` |
| 12. Quality/build gates | CI: `gofmt`, `go vet`, race tests, native + ARM64 builds |

FR1–FR10 map respectively to §§2, 4, 5, 3, 2–3, 6, 6, 7, 8, 8. Every PRD acceptance criterion must pass before release.

## 12. Definition of done

Release requires direct/proxied interoperability, valid OpenAPI, evidence for all 12 locked criteria and no gateway/IAM/business framework.
