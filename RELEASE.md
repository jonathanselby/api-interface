# API Interface — Release Checklist

## Contract

- [ ] Locked `PRD.md` retained unchanged.
- [ ] All ten functional requirements and twelve acceptance criteria proven.
- [ ] `VERSION`, `apiinterface.Version`, changelog and release tag agree.
- [ ] No gateway, IAM, authorisation, business or persistence platform embedded.
- [ ] OpenAPI paths and methods match handlers; independent OpenAPI 3.0 validator passes.
- [ ] Untrusted identity/forwarded headers cannot establish authority.
- [ ] Direct and representative gateway-backed HTTP paths behave alike.

## Engineering gate

- [ ] `go mod tidy` produces no diff.
- [ ] `gofmt` clean; `go vet ./...` passes.
- [ ] `go test -race -count=1 ./...` passes.
- [ ] Go 1.26 native build and Linux ARM64 cross-build pass.
- [ ] Live HTTP server tests prove header, body and timeout behaviour.
- [ ] Graceful shutdown, cancellation and health/readiness tests pass.

## Evidence and release

- [ ] Published source, tests and module metadata have been verified in GitHub.
- [ ] `generated/EDD.md` pinned to final source-changing commit.
- [ ] `generated/COMPARISON.md` has no unexplained mismatches.
- [ ] CI is green after generated evidence commit.
- [ ] Only then create immutable `v0.1.0` tag.
