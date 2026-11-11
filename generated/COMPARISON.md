# API Interface — Intended Design versus Recovered Implementation

**Design:** `EDD.md` v0.1  
**Locked requirements:** `PRD.md` v0.1  
**Implementation evidence commit:** `fe9d427cc97176bc709c0a733e70db2e5dc4ee63`

| Design area | Hand-written intent | Source / verification | Assessment |
|---|---|---|---|
| Component boundary | HTTP/JSON transport, no gateway or domain policy | Public `http.Handler`, example HTTP adapter and separate domain; standard-library Go module | MATCH |
| Routing | `http.ServeMux`, explicit v1 paths | Immutable route table and `ServeMux.Handle` registration | MATCH |
| OpenAPI | Registered routes and contract metadata together | `New` generates OpenAPI 3.0.3; duplicate and incomplete registrations rejected; independent CI validator | MATCH |
| Request identity | Typed hand-off from trusted middleware | `WithTrustedIdentity`, server-generated request ID; forged identity header ignored | MATCH |
| Correlation and trace | Bounded optional metadata | Token and `traceparent` validation, stable request context | MATCH |
| JSON boundaries | Strict, bounded input and typed responses | Standard-library JSON, MaxBytesReader, UTF-8/one-object checks; tests | MATCH |
| Error contract | Fixed, non-leaking JSON error mapping | Typed `Fault`, generic messages and opaque 500 | MATCH (classified API errors) |
| Idempotency | Hand-off only; domain owns replay | Bounded key parser; example application manages in-memory receipt matching | MATCH |
| Pagination | Bounded size/cursor with domain ordering | Parser validates bounds; example app owns stable ordering and cursor encoding | MATCH |
| Proxy compatibility | Direct or behind gateway; explicit trust | Trusted CIDRs and reverse-proxy integration tests | MATCH |
| Health/readiness | Process health and external readiness predicate | `/health`, `/ready` and `TestReadinessIsolation` | MATCH |
| Server resource bounds | HTTP body, headers, read/write/idle deadlines, shutdown | Core body bound; host server timeouts; live server tests including separate read-body, write and idle cases | MATCH |
| Cancellation | Propagate through application calls | Request uses original context, cancellation and client-disconnect tests | MATCH |
| Dependencies | Standard library, Go 1.26, ARM64 | No third-party Go modules; CI's OpenAPI validator is test-only | MATCH |
| Release control | CI before evidence/tag | Manual tag workflow; evidence pinned to source SHA | MATCH |

## Adversarial review and corrections

1. An earlier test suite covered `ReadHeaderTimeout`, header-size refusal and body-size refusal but did not separately exercise a stalled body read, delayed write or expired keep-alive connection. `internal/exampleapi/server_timeouts_test.go` adds three live TCP/HTTP tests with no production behaviour change.
2. The three tests passed under local race-enabled runs, including repeated execution. The source-change commit is the evidence anchor and must pass the repository's Go 1.26 CI before release.
3. The component deliberately does not implement server lifecycle policy inside the reusable package. `http.Server` owns operational timeouts, authentication middleware owns identity verification, and application code owns business state. Introducing a gateway/framework or independent timeout subsystem would increase complexity without satisfying an additional PRD requirement.

## Clarifications rather than hidden mismatches

- Standard JSON error envelopes are guaranteed for errors explicitly classified through `WriteError`; the standard Go mux may issue its native 404/405 for unmatched routes/methods. The PRD does not require replacement of all server-generated HTTP responses, but this difference is documented rather than claimed away.
- The example retry-safe mutation uses an in-memory receipt store. It demonstrates hand-off and single-process idempotency, not distributed durable idempotency.
- CI cross-compilation proves Linux ARM64 compilation, not ARM64 runtime validation or performance.
- Reverse-proxy tests prove transport compatibility and trust filtering in a representative local deployment, not certification against every commercial gateway.

## Result

No unexplained design-to-source mismatch identified in the reviewed v0.1 contract. The PRD's ten functional requirements and twelve acceptance criteria are mapped to executable checks. Release remains gated on successful CI for the cited source commit and a subsequent green documentation-only CI run.