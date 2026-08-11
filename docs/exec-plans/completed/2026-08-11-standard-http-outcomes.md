# Standard HTTP Outcomes

## Goal

Replace legacy numeric webhook reason codes with semantic reasons and standard
HTTP statuses. Keep processing outcomes, HTTP responses, logs, and Discord
notifications consistent without coupling notification severity to HTTP.

## Scope

- Replace `StatusCode` with named semantic reasons.
- Add a failure class for request, internal, and dependency failures.
- Map outcome kind and failure class to HTTP only in the HTTP adapter.
- Split overloaded reasons that currently hide distinct rejection and failure
  cases.
- Update tests, operator documentation, and the v1 migration guide.

## Non-Goals

- Change matching policy or import thresholds.
- Change Discord notification filtering or severity.
- Add retry policy for dependency failures.
- Change authentication or endpoint ownership.

## Risks

- Autobrr External webhook entries must change their expected status from `250`
  to `200`.
- Failure classification can report an incorrect standard HTTP status if it is
  applied too far from the failing operation.
- Existing tests can preserve old numeric assumptions unless response contracts
  are asserted directly.

## Steps

- [x] Audit legacy numeric reason producers, consumers, and operator guidance.
- [x] Define semantic reasons, failure classes, and outcome validation.
- [x] Replace numeric reason use across processing, release, notification, and
  torrent-client packages.
- [x] Add one HTTP adapter mapping for standard statuses and response bodies.
- [x] Update README and v1 migration guidance, including rollback behavior.
- [x] Run focused tests, full Go tests, formatting, static checks, and diff
  review.

## Decision Log

- Use semantic reason identifiers and stable user-facing messages. Do not assign
  numeric values to reasons.
- Map success to `200` and rejection to `422`.
- Map request, internal, and dependency failures to `400`, `500`, and `502`.
- Keep Discord severity and notification filtering based on outcome kind only.
- Classify failures at their source or nearest orchestration seam. Do not create
  a reason-to-HTTP lookup table.
- Use one response shape for valid processing outcomes: `outcome`, `reason`,
  and `message`.
- Classify remote destination discovery as a dependency failure. Classify
  invalid local import policy as an internal failure. Classify a torrent format
  that an adapter cannot import as a request failure.
- Rebuild the migration-guide screenshots from a disposable Autobrr 1.83.0
  instance. Keep the same full-form scope as the existing images and change the
  expected status to `200`.

## Verification Notes

- `gofumpt -w .`
- `git diff --check`
- `go test -v ./...`
- `go vet ./...`
- `go test -race ./internal/domain ./internal/http ./internal/notification ./internal/release ./internal/torrentclient`
- `govulncheck ./...`: no called vulnerabilities.
- `deadcode ./...`: four unrelated existing helpers remain unreachable.
- Live Autobrr screenshots checked at full form scope. Candidate image is
  `1260x1588`. Torrent-aware image is `1260x1638`.
