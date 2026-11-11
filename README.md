# API Interface

[![test](https://github.com/jonathanselby/api-interface/actions/workflows/test.yml/badge.svg)](https://github.com/jonathanselby/api-interface/actions/workflows/test.yml)

A small, reusable Go HTTP/JSON boundary for direct deployments or customer-managed API gateways.

> **We build the API. The customer may supply the gateway.**

The component provides versioned `net/http` endpoints, request context, bounded strict JSON decoding, stable errors, OpenAPI 3.0.3, idempotency-key and pagination conventions, trusted proxy metadata, and liveness/readiness. Business rules and authentication remain with the consuming application or trusted middleware.

## Run the example

Requires Go 1.26 for the supported build; no third-party Go modules.

```sh
go run ./cmd/api-example
```

Browse `http://127.0.0.1:8080/health` or `http://127.0.0.1:8080/openapi.json`.

```sh
curl -i -H 'Content-Type: application/json' -H 'Idempotency-Key: demo-1' \
  -d '{"title":"first"}' http://127.0.0.1:8080/api/v1/items
curl http://127.0.0.1:8080/api/v1/items?limit=2
```

The sample's item store and idempotency records are intentionally **in memory**, not durable or production business logic. It demonstrates application ownership rather than universal idempotency guarantees.

For a direct TLS listener supply `-tls-cert` and `-tls-key`; alternatively terminate HTTPS at a customer gateway. `-openapi` writes the generated OpenAPI document to stdout without starting a server.

## Public Go contract

```go
api, err := apiinterface.New(apiinterface.Config{
    Title: "My Application", Version: "1.0.0", MaxBodyBytes: 1 << 20,
    Ready: myReadinessCheck,
}, []apiinterface.Route{
    {Method: "GET", Path: "/api/v1/status", OperationID: "getStatus",
     Summary: "Get status", Responses: []apiinterface.Response{
         {Status: 200, Description: "Status"},
     }, Handler: statusHandler},
})
if err != nil { return err }
server := &http.Server{
    Handler: api, Addr: "127.0.0.1:8080", MaxHeaderBytes: 16 << 10,
    ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
    WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
}
```

All operations register handler and OpenAPI metadata together. The registration becomes immutable once built. `DecodeJSON`, `WriteJSON`, `WriteError`, `Pagination.Parse` and `IdempotencyKey` are small helpers rather than a CRUD framework.

## Security and ownership

- Authentication middleware must verify the caller, then inject `Identity` using `WithTrustedIdentity` before API Interface. **Do not** derive identity from request headers.
- Applications authorise commands and queries. Identity in context is not permission.
- Ignore `X-Forwarded-*` unless the immediate peer belongs to `TrustedProxyCIDRs`. Trusted proxies **must** strip or overwrite client-supplied forwarding headers, especially host/protocol. CIDRs are disabled by default.
- `ClientIP`, `Scheme`, and `Host` are informational metadata, never authentication or authorisation inputs. The X-Forwarded-For chain is evaluated from the trusted edge inward, stopping at the first untrusted hop.
- `MaxBodyBytes` bounds the API; handlers may enforce stricter limits. The consuming `http.Server` must separately set finite header and timeout limits.
- The application owns idempotency records, page ordering/cursor validation, business errors and readiness dependencies.
- Internal errors return a generic JSON envelope; sensitive error details are never serialised automatically.

## Engineering evidence

- [`PRD.md`](PRD.md) — locked v0.1 requirements
- [`EDD.md`](EDD.md) — simplified engineering design
- [`architecture.txt`](architecture.txt) — request and trust boundaries
- [`SKILL.md`](SKILL.md) — change discipline
- [`RELEASE.md`](RELEASE.md) — acceptance checklist
- [`generated/EDD.md`](generated/EDD.md) and [`generated/COMPARISON.md`](generated/COMPARISON.md) — generated after final source commit

CI runs formatting, vet, race tests, independent OpenAPI validation, exact version check, native and Linux ARM64 builds, and dependency/architecture gates.

## Non-goals

No API gateway, WAF, IAM, OAuth issuer, authorisation engine, customer gateway policy, schema reflection, generic CRUD, enterprise throttling, event broker, workflow engine or persistence layer.

## Licence

MIT. See `LICENSE`.
