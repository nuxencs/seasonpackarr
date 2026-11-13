# Observability consistency follow-up

## goal

Make candidate, pack, and parse diagnostics follow the operator log contract
without hiding original comparison values or expected gate outcomes.

## scope

- preserve original candidate `want` and `got` values during fuzzy comparison
- log successful and failed import-destination resolution at `INFO`
- classify expected pack rejections as informational events
- remove the superseded formatted hardlink summary
- add authenticated HTTP and release-comparison regression coverage
- update the operator log contract

## non-goals

- changing matching or threshold acceptance
- creating one common event envelope for every HTTP log
- changing configured log levels
- changing request or response payloads

## risks

- **Diagnostic drift.** Raw log values must not change normalized acceptance.
- **Outcome drift.** Only expected gate decisions can move from error to info.
- **Missing failure time.** Destination failures must retain duration and error.

## steps

1. Preserve raw candidate mismatch values. [done]
2. Log every destination-resolution attempt at `INFO`. [done]
3. Align expected pack rejection levels with candidate. [done]
4. Remove the duplicate hardlink summary. [done]
5. Update docs and verify affected packages and the full repository. [done]
6. Review standards and specification axes, address findings, and commit. [done]

## decision log

- 2026-08-12: use authenticated HTTP logs as the operator-facing test seam.
- 2026-08-12: use the release comparison seam to prove fuzzy matching still
  accepts normalized values while diagnostics retain original values.
- 2026-08-12: keep the existing event interfaces. Do not introduce a general
  logging wrapper for five local fixes.

## verification notes

- Candidate fuzzy-source and HDR tests prove original diagnostic values and
  unchanged normalized acceptance.
- Authenticated HTTP tests cover candidate diagnostics, destination success and
  failure, expected pack rejections, and removal of the duplicate summary.
- `gofumpt -w .` and `git diff --check` pass.
- `go test -v ./...` passes. Environment-gated live-client tests are skipped
  because their external test environments are not configured.
- `go test -race ./internal/release ./internal/http` passes.
- `go vet ./...` passes.
- `govulncheck ./...` reports no called-code or imported-package
  vulnerabilities. One required-module vulnerability is not called.
- Final standards review found no hard violations. Its test-helper duplication
  judgment call was rejected because local setup is clearer than another test
  abstraction.
- Final specification review found missing normalized-acceptance assertions.
  WEB and HDR acceptance cases were added and pass.
