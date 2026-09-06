# Backfill cooldowns

## Goal

Respect tracker retry deadlines and temporary failures across backfill runs.

## Scope

Prowlarr error classification, in-memory cooldowns, tests, and operator docs.

## Non-goals

Disk persistence, exponential backoff, automatic request retries, parser changes,
live searches, and deployment.

## Risks

Cooldowns must not expose credentials or block unaffected indexers. Cancellation
must not create a cooldown. Restart resets all cooldowns.

## Steps

1. Classify 429, 408, 5xx, transport failures, and interrupted response reads.
2. Honor seconds and HTTP-date Retry-After directly. Use a 10m fallback for 429
   and temporary errors only when the header is missing or invalid.
3. Reuse the existing in-memory deadline map across runs.
4. Test search and download failures, subsequent runs, expiry, and restart reset.
5. Update docs and run affected backend checks.

## Decision log

- Server deadlines with a fixed fallback; no automatic retries. Canceled requests do not create cooldowns.
- Keep state in memory. Restart and Prowlarr URL or API key changes reset it.
- Scope discovery failures to Prowlarr; scope search and download failures to the
  affected indexer. Permanent errors keep the existing per-run behavior.

## Verification notes

- Completed all steps.
- Passed `go test -race ./internal/prowlarr ./internal/http` and the application build.
- Tests cover retry headers, fallback delays, temporary transport and response-read
  failures, cancellation, cooldowns across runs, expiry, restart reset, and
  continued access to unaffected indexers.
- Changed Go files formatted with gofumpt; `git diff --check` passed.
