# Endpoint names

## Goal

Name the public operations `candidate`, `match`, and `import` before the
current PR stack reaches `develop`.

## Scope

- Replace `/api/pack` with `/api/match` and `/api/parse` with `/api/import`.
- Update CLI commands, handler names, logs, notifications, tests, and current docs.
- Document the required webhook changes in the v1 upgrade guide.

## Non-goals

- No aliases for the old routes or CLI commands.
- No changes to authentication, payloads, status codes, matching, or import behavior.
- Preserve historical plans, source audits, and v0.16.0 rollback instructions.

## Risks

- Existing webhook URLs and scripts must change with the service upgrade.
- Setup screenshots can retain old endpoint names after text changes.

## Steps

1. Complete: rename code and current documentation together.
2. Complete: cover new route authentication, rejected old routes, and CLI requests.
3. Complete: run affected Go package tests, formatting, and CLI smoke checks.
4. Complete: review the diff and move this plan to completed.

## Decision log

- 2026-09-05: The user approved a breaking rename without aliases in the
  existing unmerged PR stack.
- 2026-09-05: Retake all three setup illustrations in a local autobrr v1.83.0
  instance, as requested by the user. Use consistent native 2x resolution and
  restore the guide references with the current endpoint values.
- The CLI smoke check showed that unknown operations printed help and exited
  successfully. Add argument validation to the parent `test` command so removed
  operations return an error. Calling `test` without an operation still shows help.

## Verification notes

- `gofumpt -w .`: complete. Format the two later CLI edits with gofumpt as well.
- `go test -v ./cmd ./internal/http ./internal/payload ./internal/config`: pass.
  Repeat `go test -v ./cmd` after adding unknown-operation validation: pass.
- `govulncheck ./cmd ./internal/http ./internal/payload ./internal/config`:
  no reachable vulnerabilities. Four module-level advisories have no affected
  imported packages or called symbols in this scan.
- Build the CLI and send requests to a local HTTP recorder: candidate, match,
  and import use the expected URL, API token, client name, and payload. Match
  and import preserve supplied torrent-file bytes. Removed commands return an
  unknown-command error. Help lists only the three current operations.
- HTTP component tests cover matching, plan reuse, real temporary hardlinks,
  and import through a mock torrent client. Missing or incorrect authentication
  cannot reach client or filesystem operations. Old routes return `404` without
  redirects or mutations.
- `git diff --check`: pass.
- No external autobrr or real torrent-client E2E run. Verification used the
  local HTTP recorder and the existing mock-client component fixtures.
- Screenshot follow-up: run the cached official autobrr v1.83.0 Docker image
  on loopback with disposable data. Configure the documented webhook payloads
  through its API and inspect the real forms in Chrome. Verify candidate-first
  order after reload. Capture at native 2x resolution with matching horizontal
  framing: candidate 2540 x 3254 pixels, exact match 2540 x 3302 pixels,
  import action 2540 x 2258 pixels. Inspect all three images at source resolution
  and restore the guide references.
  This verifies setup presentation only; no tracker or torrent client is connected.
