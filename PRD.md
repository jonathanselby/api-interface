# API Interface --- Product Requirements Document

**Version:** 0.1

## 1. Problem

Garage applications need a conventional machine interface that works
directly for small deployments and fits behind customer API gateways in
government and enterprise environments.

If each application invents HTTP conventions, integration becomes
expensive. If Garage builds a gateway, it duplicates customer
infrastructure and violates the mini-monolith model.

## 2. Product objective

Provide a small Go HTTP/JSON API boundary with a stable request context,
error contract and OpenAPI description while leaving business logic and
enterprise gateway policy outside the component.

> **We build the API. The customer may supply the gateway.**

The V0.1 product question is: can Garage applications expose a
consistent enterprise-compatible API without creating an API-management
platform?

## 3. Users

Garage applications, customer integration teams, external systems and
developers integrating through HTTP APIs.

## 4. In scope

-   HTTPS-capable HTTP server integration;
-   JSON request/response conventions;
-   REST-style resource and command endpoints;
-   versioned API paths/contracts;
-   OpenAPI document generation/serving;
-   request ID and correlation ID;
-   authenticated identity context hand-off;
-   idempotency-key support for appropriate mutations;
-   pagination conventions;
-   stable error envelope;
-   request/body/time limits;
-   health/readiness endpoints;
-   graceful shutdown;
-   proxy/gateway-aware configuration;
-   tests behind a representative reverse proxy;
-   Go 1.26 and `linux/arm64` build proof.

## 5. Out of scope

API gateway, WAF, developer portal, enterprise OAuth server, IAM,
service mesh, generic integration bus, arbitrary payload transformation,
business authorisation, generic CRUD framework, rate-limit platform, API
monetisation and customer-specific gateway policy.

## 6. Functional requirements

### FR1 --- Thin transport

Handlers parse transport input, construct application commands/queries,
invoke application behaviour and serialise results. Business logic must
not live in HTTP handlers.

### FR2 --- Stable request context

Every request can carry request ID, correlation ID, authenticated
identity reference, client reference, receive time and trace context.

### FR3 --- Error contract

Errors use a small stable JSON envelope and documented HTTP status
mapping without leaking stack traces, SQL or internal secrets.

### FR4 --- OpenAPI

The application can expose or generate an OpenAPI contract for its
registered API surface.

### FR5 --- Versioning

Public API contracts are explicitly versioned.

### FR6 --- Idempotency

Endpoints that can safely support client retries may accept an
idempotency key and pass it to application semantics. The API layer must
not pretend all mutations are idempotent.

### FR7 --- Pagination

Collection endpoints use a consistent bounded pagination convention.

### FR8 --- Gateway compatibility

The API operates directly or behind common reverse proxies/API gateways
without domain changes.

### FR9 --- Limits and cancellation

Request size, header size and server timeouts are bounded; cancellation
propagates to application work.

### FR10 --- Health/readiness

Operational health endpoints are separate from business APIs and reveal
no sensitive application data.

## 7. Acceptance criteria

V0.1 is accepted when executable evidence demonstrates:

1.  an example application exposes versioned JSON API endpoints;
2.  handlers contain transport translation rather than domain rules;
3.  request/correlation/identity context reaches application execution;
4.  documented error statuses/envelopes are stable;
5.  OpenAPI describes the example API and validates;
6.  idempotency behaviour is explicit on a retry-safe mutation;
7.  pagination is bounded and deterministic;
8.  request cancellation reaches application execution;
9.  request/body/time limits are enforced;
10. the same API works directly and behind a representative reverse
    proxy;
11. no API-gateway/IAM platform is embedded;
12. `go vet`, race tests, native build and `linux/arm64` build pass.

## 8. Definition of done

V0.1 is complete when a Garage application can expose a documented API
directly or behind an enterprise gateway without changing domain code,
every acceptance criterion has named evidence, and the component remains
an HTTP boundary rather than an API-management framework.
