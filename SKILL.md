# API Interface — Skill

## Use this when

Changing HTTP/JSON transport, route registration, OpenAPI, request context, error mapping, proxy handling, health/readiness or resource bounds.

## Core rule

> **We build the API. The customer may supply the gateway.**

## Change discipline

1. Preserve the locked v0.1 PRD, including all ten functional requirements and twelve acceptance criteria.
2. Keep handlers thin; application code owns authorisation, validation, persistence, cursor semantics and idempotency replay.
3. Use `net/http`; do not add a web framework, gateway, IAM, CRUD engine or middleware registry.
4. Register handlers and OpenAPI metadata together; reject invalid/duplicate registrations.
5. Only trusted authenticated middleware may inject identity into Go context; never accept identity from ordinary HTTP or forwarded headers.
6. Trust forwarding metadata only through explicit immediate-peer CIDRs; never promote it to business authority.
7. Bound JSON inputs, pagination, keys and server resources. Keep cancellation attached to `r.Context()`.
8. Do not expose internal error strings, dependency diagnostics or sensitive readiness details to clients.
9. Keep standard-library-only runtime dependencies unless a measurable requirement defeats them.
10. Run format, vet, race, live-server integration, OpenAPI validation, native and ARM64 acceptance before release.
11. Final implementation evidence must be pinned to the actual source-changing GitHub commit.
12. Build and test complete changes offline; publish in coherent commits. No recursive Git transport experiments.
