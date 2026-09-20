# Persistent Discovery State

## Goal

Preserve discovery progress across restarts with embedded SQLite. Keep setup automatic and preserve current matching, CLI, and webhook behavior.

## Scope

- New branch above `feat/prowlarr-backfill`.
- One SQLite store with automatic schema migrations and private file permissions.
- Persist RSS checkpoints and retained candidates, metadata, and cooldown deadlines.
- Keep existing expiry, capacity, connection-reset, and client-recovery rules.
- Restart and failure regression tests, build compatibility checks, and operator documentation.

## Non-goals

- Import jobs, automatic import recovery, search history, or asynchronous search.
- Database configuration, alternative storage backends, or a persistent client inventory.
- Changes to matching decisions or filesystem mutations.

## Risks

- A checkpoint must not advance without retained candidates being committed in the same transaction.
- Client failures must not consume feed entries for unavailable clients.
- Stored metadata and links can contain tracker credentials.
- Storage failures must be visible and must not silently switch to volatile state.
- The driver must support the existing CGO-free release targets, including Linux ARMv6 and FreeBSD amd64.

## Steps

1. [x] Check SQLite driver and storage location against packaging.
2. [x] Implement the store, schema, retention, and persistence tests.
3. [x] Integrate discovery and application lifecycle; add restart and failure tests.
4. [x] Update operator and architecture documentation.
5. [x] Run affected tests, race tests, vulnerability checks, and release-target builds.

## Decisions

- Keep import jobs in a later change.
- Keep configuration in YAML and compute the database location automatically.
- SQLite owns durable state. Per-run RSS working state and cooldown maps are not independent caches.
- Do not persist acceptance decisions or exact import plans.
- Use `modernc.org/sqlite` with its matching libc dependency. CGO-free builds preserve all release targets.
- Store the database beside the active config. Environment-only installs use the explicit config directory or XDG data directory, with the home data directory as fallback.
- Use a single SQL connection, WAL journaling, automatic SQLite checkpoints, and FULL synchronization. WAL replaces the initial conservative DELETE choice after user review. FULL preserves committed writes through OS crashes or power loss when storage honors synchronization. No custom checkpoint worker is needed.
- Preserve the RSS working-set rules. Commit candidates and checkpoints atomically before evaluation; do not commit feed progress for unavailable clients.
- Store metadata directly in SQLite, not in a second process-local cache. Keep existing bounds and LRU behavior.
- Stop discovery on storage failures. Reject corrupt or unsupported newer databases at startup.
- Use an API-key-keyed HMAC-SHA-256 of the Prowlarr URL for connection identity. CodeQL classified the initial plain SHA-256 of the URL and API key as password hashing; use the credential as a cryptographic key instead. No alert suppression is needed.
- Config values and defaults did not change, so `config.yaml` and the config schema require no new fields.

## Verification

All Go checks used `GOTOOLCHAIN=go1.26.6`.

- `go test -race ./...`: passed after storage integration.
- `go test -race ./internal/state ./internal/http ./cmd`: passed after final regression additions.
- `go vet ./...`: passed.
- `go fix -diff ./internal/state ./internal/http ./cmd`: no changes suggested.
- `gofumpt` on changed Go files and `go mod tidy -v`: complete.
- `govulncheck ./...`: no reachable symbol or imported-package vulnerabilities. Four advisory matches exist in required modules without reachable calls.
- `deadcode -test ./...`: reports the pre-existing `internal/release/episode.go` `EpisodeMatcher.Len`; no new dead code.
- `go test -cover ./internal/state`: 85.7 percent statement coverage.
- CGO-disabled binary builds passed for Linux amd64, ARMv6, arm64; Darwin amd64, arm64; Windows amd64; and FreeBSD amd64.
- Binary smoke checks passed: startup, health endpoint, graceful shutdown, private database permissions, preserved cooldown after restart, environment-only data paths, and rejection of a newer schema.
- CLI preview/import fixture reopens the database between preview and import and confirms metadata reuse.
- Regression coverage includes RSS catch-up after restart, retained-candidate reevaluation, unavailable-client recovery, cooldown preservation, atomic rollback, metadata bounds and expiry, connection invalidation, and storage failures before mutation.
- No live tracker or external torrent-client calls were needed or performed. Native execution on non-host release targets remains unverified; cross-compilation passed.
