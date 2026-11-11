# API Interface — Implementation-Derived Engineering Evidence

**Source:** review of published Go source, tests and CI workflow  
**Evidence commit:** `fe9d427cc97176bc709c0a733e70db2e5dc4ee63`  
**Target:** v0.1.0, Go 1.26

## Recovered component boundary

```text
http.Server (host application)
       |
       v
API Interface (apiinterface.API implements http.Handler)
       |-- net/http ServeMux + versioned route registrations
       |-- request context (opaque request ID, trusted identity hand-off)
       |-- bounded JSON parsing, error mapping, page/key conventions
       |-- OpenAPI 3.0.3 document from route registrations
       |-- health + caller-provided readiness check
       |
       v
exampleapi (HTTP translation) -> exampleapp (domain logic / in-memory state)

optional customer proxy -> http.Server (no gateway code inside component)
```

`api.go`, `context.go` and `contract.go` expose the public Go package. `internal/exampleapi/http.go` demonstrates HTTP-to-service translation; `internal/exampleapp/app.go` owns example validation, idempotency receipts, item state, pagination order and cursor logic. `cmd/api-example/main.go` owns the HTTP server and its lifecycle. There is no framework, API gateway, IAM provider, database or background worker.

## Recovered public API and state

`New(Config, []Route)` builds an `API` containing one `http.ServeMux`, a read-only generated OpenAPI byte slice, trusted proxy CIDRs and a body limit. Route registrations supply method, path, unique operation ID, description, parameters, request/response JSON schemas and handler. Duplicate or incomplete registrations are rejected. Route registration and API contract construction are one operation; no mutable runtime registry exists.

`RequestContext` carries request ID, correlation ID, identity/client references, receive time, traceparent and informational client address/scheme/host. The request ID is randomly generated. Authentication is not implemented; `WithTrustedIdentity` requires a trusted caller middleware to insert identity into `context.Context`. Unverified identity headers cannot do so.

JSON helpers use standard-library encoding and finite body limits. Input decoding requires an `application/json` media type, valid UTF-8, one JSON value and known command fields. Response errors are classified into typed `Fault` values with fixed HTTP status and generic public messages; untyped internal errors become an opaque 500 response.

`IdempotencyKey` validates and returns an optional bounded header, without storage or retries. `Pagination.Parse` validates a maximum page size and an opaque bounded cursor; the application owns cursor interpretation.

## Recovered request execution

```text
HTTP request
  -> requestContext (new ID; validate optional correlation / trace)
  -> global MaxBytesReader
  -> net/http ServeMux
  -> versioned thin handler
  -> DecodeJSON / IdempotencyKey / Pagination.Parse as needed
  -> application call with original cancellation context
  -> typed result or classified error -> JSON response
```

The API also serves `/openapi.json`, `/health` and `/ready`. Liveness does not depend on the application; readiness uses an injected callback and suppresses diagnostic details.

## Proxy, authentication and operational boundaries

The immediate network peer must match configured CIDRs before `X-Forwarded-*` metadata is used; otherwise forwarded fields are ignored. X-Forwarded-For is walked backwards through trusted hops. Forwarded metadata is informational and cannot establish identity or permission. The documented deployment contract requires trusted proxies to strip/replace untrusted forwarded host/protocol fields.

`http.Server` outside the package owns TLS, `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes` and graceful `Shutdown`. The example binds to loopback by default; it can use direct TLS or sit behind an upstream HTTPS-terminating gateway. The API package implements `http.Handler`, not a server or proxy.

## Recovered acceptance evidence

| Locked PRD acceptance | Executable evidence |
|---|---|
| 1. Versioned JSON API | `TestExampleAPI` |
| 2. Thin handlers | `TestArchitectureBoundary`; domain implementation resides in `internal/exampleapp` |
| 3. Context reaches application | `TestRequestContextPropagation` (core and integration) |
| 4. Stable errors | `TestErrorContract`, `TestDecodeStrictJSONAndLimits` |
| 5. OpenAPI generated and validated | `TestOpenAPIValidation`, independent CI OpenAPI 3.0 validator |
| 6. Retry-safe idempotency | `TestIdempotencyKey` in both layers |
| 7. Bounded deterministic pagination | `TestPaginationContract` in both layers |
| 8. Cancellation propagation | `TestRequestCancellation`, `TestClientDisconnectPropagatesContext` |
| 9. Limits | `TestRequestLimits`, `TestReadBodyTimeout`, `TestWriteTimeout`, `TestIdleTimeout` |
| 10. Direct and reverse proxy | `TestBehindReverseProxy`, `TestTrustedProxyMetadata` |
| 11. No gateway/IAM implementation | `TestDependencyBoundary`, import/dependency review |
| 12. Go/race/native/ARM64 | `.github/workflows/test.yml` Go 1.26 quality gates |

26 top-level named tests exist in the source snapshot (13 core, 13 example/integration), together with an independent CI OpenAPI 3.0 validator. CI validates module tidiness, formatting, vet, race-enabled tests, example OpenAPI production/validation, native build and Linux ARM64 build.

## Explicit limitations and ownership

- The sample application uses an in-memory item/idempotency store for demonstration only. It makes no durability or multi-instance replay claim.
- Consumer applications must provide real authentication, authorisation, business validation, durable idempotency where needed and cursor correctness.
- Proxies must be explicitly trusted and properly sanitise forwarded metadata; the component does not configure gateway infrastructure.
- Read, write, idle and header deadlines depend on the consuming `http.Server`; this is deliberate and verified using live-server tests.
- No GraphQL, WebSockets, SSE, generic CRUD, gateway, enterprise rate limiting or vendor adapter is implemented.
- A native `net/http` unmatched route/method may use its default 404/405 response; the stable JSON error envelope is for API-classified faults, not a custom replacement for every HTTP server-generated response.

This file records observed source and test structure, not evidence of a real customer deployment or performance under production load.